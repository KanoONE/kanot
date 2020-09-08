/*  Copyright 2020 The Kano Terminal Authors

    This file is part of kanot.

    kanot is free software: you can redistribute it and/or modify
    it under the terms of the GNU Affero General Public License as
    published by the Free Software Foundation, either version 3 of the
    License, or (at your option) any later version.

    kanot is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU Affero General Public License for more details.

    You should have received a copy of the GNU Affero General Public License
    along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package kanot

import (
	"time"
	"context"
	"strings"
	"math/big"
	
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/jackc/pgx/v4/pgxpool"
)

const (
	endpoint = "ws://127.0.0.1:13516"
	subChanSize = 1024
)

var (
	usPairABI abi.ABI
)

type UniswapPair struct {
	token1, token2 string
	addr string
}

func (up *UniswapPair) DBTableName(eventName string) string {
	return "us_" + up.token1 + "_" + up.token2 + "_" + eventName
}

func init() {
	// load ABI
	var err error
	usPairABI, err = abi.JSON(strings.NewReader(uniswapPairABI))
	if err != nil {
		log.Error("abi.JSON", "err", err)
		panic(err)
	}
}

func StreamETH() {
	config, err := pgxpool.ParseConfig(dbConnString)
	if err != nil {
		log.Error("pgxpool.ParseConfig", "err", err)
		panic(err)
	}
	hours, _ := time.ParseDuration("168h")
	config.MaxConnLifetime = hours
	config.MaxConnIdleTime = hours
	config.MaxConns = 10
	
	dbPool, err := pgxpool.ConnectConfig(context.Background(), config)
	if err != nil {
		log.Error("pgxpool.Connect", "err", err)
		panic(err)
	} else {
		log.Info("pgxpool.Connect OK")
		//defer dbConn.Close(context.Background())
	}

	pairs := []UniswapPair{
		UniswapPair{"USDC", "ETH", "0xb4e16d0168e52d35cacd2c6185b44281ec28c9dc"},
		UniswapPair{"ETH", "USDT", "0x0d4a11d5eeaac28ec3f61d100daf4d40471f1852"},
		UniswapPair{"DAI", "ETH", "0xa478c2975ab1ea89e8196811f51a7b7ade33eb11"},
		UniswapPair{"YFI", "ETH", "0x2fDbAdf3C4D5A8666Bc06645B8358ab803996E28"},
		UniswapPair{"SUSHI", "ETH", "0xce84867c3c02b05dc570d0135103d3fb9cc19433"},
		UniswapPair{"LINK", "ETH", "0xa2107fa5b38d9bbd2c461d6edf11b11a50f6b974"},
	}
	
	for i := 0; i < len(pairs); i++ {
		p := pairs[i]

		dbConn, err := dbPool.Acquire(context.Background())
		if err != nil {
			log.Error("pgxpool.Pool.Acquire", "err", err)
			panic(err)
		}
		
		go func() {
			stream(p, dbConn)
		}()
	}
}

func stream(pair UniswapPair, dbConn *pgxpool.Conn) {
	// https://godoc.org/github.com/ethereum/go-ethereum/ethclient
	client, err := ethclient.Dial(endpoint)
	if err != nil {
     	log.Error("rpc.Dial", "err", err)
		return
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// https://godoc.org/github.com/ethereum/go-ethereum#FilterQuery
	fq := ethereum.FilterQuery{
		BlockHash: nil,
		FromBlock: nil,
		ToBlock: nil,
		Addresses: []common.Address{common.HexToAddress(pair.addr)},
	}
	
	ch := make(chan types.Log, subChanSize)
	
	sub, err := client.SubscribeFilterLogs(ctx, fq, ch)
	if err != nil {
		log.Error("ethclient.SubscribeFilterLogs", "err", err)
		return
	}
	defer sub.Unsubscribe()

	log.Info("Streaming Uniswap V2", "pair", pair.token1 + "-" + pair.token2, "addr", pair.addr)
	for l := range ch {
		// get block timestamp
		h, err := client.HeaderByHash(context.Background(), l.BlockHash)
		if err != nil {
			log.Error("client.HeaderByHash", "err", err)
			return
		}
		handleEvent(l, h.Time, pair, dbConn)
	}
}

// Unpack events from their data and/or topics.  See solidity docs on indexing:
// https://solidity.readthedocs.io/en/v0.7.1/contracts.html#events
// And go-ethereum docs on event reading:
// https://goethereumbook.org/event-read/
func handleEvent(l types.Log, blockTime uint64, pair UniswapPair, dbConn *pgxpool.Conn) {
	//log.Info("Uniswap Pair YFI-ETH", "txhash", l.TxHash, "topics", l.Topics, "data", l.Data)
	var err error
	var eventName string
	// Sync event is the only unindexed event
	if len(l.Topics) == 0 {
		eventName = "Sync"
	} else {
		// Otherwise the first topic identifies the event
		ev, err := usPairABI.EventByID(l.Topics[0])
		if err != nil {
			log.Error("usPairABI.EventByID", "err", err)
			return
		}
		eventName = ev.RawName
	}

	m := make(map[string]interface{})
	err = usPairABI.UnpackIntoMap(m, eventName, l.Data)
	if err != nil {
		log.Error("ABI.Unpack", "err", err, "l", l)
		return
	}

	// TODO: clean this up
	tm := make(map[string]common.Address)
	switch eventName {
	case "Sync":
		//event := UniswapPairSync{}
	case "Transfer":
		//event := UniswapPairTransfer{}
		tm["from"] = common.HexToAddress(l.Topics[1].Hex())
		tm["to"] = common.HexToAddress(l.Topics[2].Hex())
	case "Mint":
		//event := UniswapPairMint{}
		tm["sender"] = common.HexToAddress(l.Topics[1].Hex())
	case "Burn":
		//event := UniswapPairBurn{}
		tm["sender"] = common.HexToAddress(l.Topics[1].Hex())
		tm["to"] = common.HexToAddress(l.Topics[2].Hex())
	case "Swap":
		//event := UniswapPairSwap{}
		tm["sender"] = common.HexToAddress(l.Topics[1].Hex())
		tm["to"] = common.HexToAddress(l.Topics[2].Hex())
	}

	switch eventName {
	case "Sync":
		//log.Info("Sync", "reserve0", m["reserve0"], "reserve1", m["reserve1"])
		break
	case "Transfer":
		//log.Info("Transfer", "from", tm2["from"], "to", tm2["to"], "value", m["value"])
		v := m["value"].(*big.Int)
		insertTransfer(dbConn, pair, blockTime, l.TxHash.Hex(), tm["from"].Hex(), tm["to"].Hex(), v)
	case "Mint":
		//log.Info("Mint", "sender", tm2["sender"], "amount0", m["amount0"], "amount1", m["amount1"])
		break
	case "Burn":
		//log.Info("Mint", "sender", tm2["sender"], "to", tm2["to"], "amount0", m["amount0"], "amount1", m["amount1"])
		break
	case "Swap":
		/*
		log.Info("Swap",
			"sender", tm2["sender"],
			"to", tm2["to"],
			"amount0In", m["amount0In"],
			"amount1In", m["amount1In"],
			"amount0Out", m["amount0Out"],
			"amount1Out", m["amount1Out"])
*/
		a0In, a1In, a0Out, a1Out := m["amount0In"].(*big.Int), m["amount1In"].(*big.Int), m["amount0Out"].(*big.Int), m["amount1Out"].(*big.Int)
		insertSwap(dbConn, pair, blockTime, l.TxHash.Hex(), tm["sender"].Hex(), tm["to"].Hex(), a0In, a1In, a0Out, a1Out)
	}
	//log.Info("Unpacked:", "name", eventName, "topics", tm2, "data", m)
}
