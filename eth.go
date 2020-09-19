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
	"context"
	"time"
	"math/big"
	
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	//"github.com/jackc/pgx/v4/pgxpool"
)

const (
	//
	// Ethereum
	//
	// websocket endpoint of Ethereum full node
	endpoint = "ws://127.0.0.1:13516"

	// number of blocks for high guarantee of no reorgs
	blockConfirmations = 30 // around 6 minutes

	//
	// PostgreSQL
	//
	dbConnString = "host=127.0.0.1 port=5432 dbname=dev1 user=kanot password=kanot"

	// used for both pgxpool.Config.MaxConnLifetime and pgxpool.Config.MaxConnIdleTime
	// TODO: for now set high so conns are open indefinitely
	pgxMaxConnTime = "876000h" // 100 years

	// TODO: increase when we implement concurrent inserts
	pgxMaxConns = 1

	//
	// Performance Tuning
	//
	pollingCycle = 600 * time.Second
	
	queryBlockCount = 2048 //6646*1 // ~1 day worth of blocks
	queryTimeout = 120 * time.Second
)

func SyncETH() {
	initDBPool()
	
	usFactory := UniswapV2FactoryPairCreated()
	
	eventSyncs := []*EventSync{
		usFactory,
	}

	for _, es := range eventSyncs {
		go func() {
			for {
				log.Info("Syncing", "eventName", es.EventName)
				syncEvent(es)
				time.Sleep(pollingCycle)
			}
		}()
	}

	// TODO: go routine for all pairs:
	//usPair := UniswapV2Pair(pairTicker, addr, createBlock, eventName)

}

func syncEvent(es *EventSync) {
	lastInsertedBlock := dbQueryUint64(es.DBBlockQuery, []interface{}{})
	
	client, err := ethclient.Dial(endpoint)
	if err != nil {
     	log.Error("rpc.Dial", "err", err)
		return
	}

	latestHeader, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Error("client.HeaderByNumber", "err", err)
		return
	}
	
	var fromBlock, toBlock uint64
	maxBlock := latestHeader.Number.Uint64() - blockConfirmations

	// toBlock is used as fromBlock in first iteration
	if lastInsertedBlock == 0 {
		toBlock = es.ContractCreateBlock - 1
	} else {
		toBlock = lastInsertedBlock
	}
	
	for {
		fromBlock = toBlock + 1
		toBlock = fromBlock + queryBlockCount
		if toBlock > maxBlock {
			toBlock = maxBlock
		}

		syncLogs(client, es, fromBlock, toBlock)
		
		if toBlock == maxBlock {
			break
		}
	}
}

func syncLogs(client *ethclient.Client, es *EventSync, fromBlock, toBlock uint64) {
	addrs := []common.Address{es.ContractAddr}
	logs := make([]types.Log, 0)
	
	// https://godoc.org/github.com/ethereum/go-ethereum#FilterQuery
	fq := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock: new(big.Int).SetUint64(toBlock),
		Addresses: addrs}
	
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	t0 := time.Now()
	logs1, err := client.FilterLogs(ctx, fq)
	if err != nil {
		log.Error("ethclient.FilterLogs", "err", err)
		return
	}
	t1 := time.Since(t0)
	
	logs = append(logs, logs1...)
	
	log.Info("syncEvent", "fromBlock", fromBlock, "toBlock", toBlock, "logs", len(logs1), "total", len(logs), "t", t1)

	for _, l := range logs1 {
		args := make([]interface{}, 0)
		args = append(args, l.BlockNumber)

		for i, _ := range es.LogTopicNames {
			addr := common.HexToAddress(l.Topics[i+1].Hex())
			args = append(args, addr.Hex())
		}

		m := make(map[string]interface{})
		err = es.ContractABI.UnpackIntoMap(m, es.EventName, l.Data)
		if err != nil {
			log.Error("ABI.Unpack", "err", err, "es.EventName", es.EventName, "l", l, "abi", *es.ContractABI)
			return
		}

		i := 0
		for i < len(es.LogDataNamesAndTypes) {
			switch es.LogDataNamesAndTypes[i+1] {
			case "address":
				a := m[es.LogDataNamesAndTypes[i]].(common.Address)
				args = append(args, a.Hex())
			case "smalluint":
				b := m[es.LogDataNamesAndTypes[i]].(*big.Int)
				args = append(args, b.Uint64())
			case "biguint":
				b := m[es.LogDataNamesAndTypes[i]].(*big.Int)
				args = append(args, BigToFloat(b))
			}
			i += 2
		}

		dbExec(es.DBInsertQuery, args)
	}
}



	
	
	/*
	//log.Info("Streaming Uniswap V2", "pair", pair.token1 + "-" + pair.token2, "addr", pair.addr)
	for l := range ch {
		h, err := client.HeaderByHash(context.Background(), l.BlockHash)
		if err != nil {
			log.Error("client.HeaderByHash", "err", err)
			return
		}

		token0 := common.HexToAddress(l.Topics[1].Hex())
		token1 := common.HexToAddress(l.Topics[1].Hex())

		m := make(map[string]interface{})
		err = usPairABI.UnpackIntoMap(m, "PairCreated", l.Data)
		if err != nil {
			log.Error("ABI.Unpack", "err", err, "l", l)
			return
		}

		pairAddr := m["pair"].(common.Address)
	
	}
*/

/*
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

/*
		a0In, a1In, a0Out, a1Out := m["amount0In"].(*big.Int), m["amount1In"].(*big.Int), m["amount0Out"].(*big.Int), m["amount1Out"].(*big.Int)
		insertSwap(dbConn, pair, blockTime, l.TxHash.Hex(), tm["sender"].Hex(), tm["to"].Hex(), a0In, a1In, a0Out, a1Out)
	}
	//log.Info("Unpacked:", "name", eventName, "topics", tm2, "data", m)
}
*/
