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
	//"encoding/hex"
	"strings"
	"os"
	
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/common"
	//"github.com/ethereum/go-ethereum/core/types"
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

func InitLog() {
	//log.StreamHandler(os.Stderr, log.TerminalFormat(true)),
	log.Root().SetHandler(log.MultiHandler(
		//log.MatchFilterHandler("pkg", "app/kanot", log.StdoutHandler),
		log.LvlFilterHandler(log.LvlInfo, log.StreamHandler(os.Stderr, log.TerminalFormat(true)))))
}

func SyncETH() {
	initDBPool()
	
	contractSyncs := []ContractSync{
		//NewGlueUSV2Factory(),
	}

	for _, cs := range contractSyncs {
		go func() {
			for {
				//log.Info("Syncing", "eventName", es.EventName)
				syncContract(cs)
				time.Sleep(pollingCycle)
			}
		}()
	}
	
	go syncUSV2Pairs()
}

func syncContract(cs ContractSync) {
	_, createBlock, _ := cs.Contract()
	lastInsertedBlock := cs.DBLastInsertedBlock()

	// Re-init log to avoid trace log level in go-ethereum/eth/handler.go
	//InitLog()
	
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
		toBlock = createBlock - 1
	} else {
		toBlock = lastInsertedBlock
	}
	
	for {
		fromBlock = toBlock + 1
		toBlock = fromBlock + queryBlockCount
		if toBlock > maxBlock {
			toBlock = maxBlock
		}

		syncLogs(client, cs, fromBlock, toBlock)
		
		if toBlock == maxBlock {
			break
		}
	}
}

func syncLogs(client *ethclient.Client, cs ContractSync, fromBlock, toBlock uint64) {
	contractAddr, _, contractABI := cs.Contract()
	addrs := []common.Address{contractAddr}
	
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
	
	log.Info("syncLogs", "name", cs.Name(), "fromBlock", fromBlock, "toBlock", toBlock, "logs", len(logs1), "t", t1)

	for _, l := range logs1 {
		args := make([]interface{}, 0)
		args = append(args, l.BlockNumber, l.TxHash.Hex())

		eventName := cs.EventName(l.Topics)
		tn, dnt := cs.LogFields(eventName)
		
		for i, _ := range tn {
			addr := common.HexToAddress(l.Topics[i+1].Hex())
			args = append(args, addr.Hex())
		}

		m := make(map[string]interface{})
		err = contractABI.UnpackIntoMap(m, eventName, l.Data)
		if err != nil {
			log.Error("ABI.Unpack", "err", err, "es.EventName", eventName, "l", l, "abi", contractABI)
			return
		}

		i := 0
		for i < len(dnt) {
			switch dnt[i+1] {
			case "address":
				a := m[dnt[i]].(common.Address)
				args = append(args, a.Hex())
			case "smalluint":
				b := m[dnt[i]].(*big.Int)
				args = append(args, b.Uint64())
			case "biguint":
				b := m[dnt[i]].(*big.Int)
				args = append(args, BigToFloat(b))
			}
			i += 2
		}

		cs.DBInsert(client, &l, args)
	}
}

func syncUSV2Pairs() {
	q := "SELECT * FROM us_factory ORDER BY block ASC LIMIT 1"
	pairs := dbQueryPairsCreated(q, []interface{}{})
	log.Info("syncUSV2Pairs", "pairs", len(pairs), "first", pairs[0])

	glue := NewGlueUSV2Pair(common.HexToAddress(pairs[0].pair_addr), pairs[0].block, pairs[0].ticker)
	syncContract(glue)
	
}

func getSymbol(ec *ethclient.Client, addr string) string {
	// Use the USV2Pair ABI as it has the standard ERC-20 Symbol function
	c0, err := NewUSV2Pair(common.HexToAddress(addr), ec)
	if err != nil {
		log.Error("NewUSV2Pair", "err", err)
		panic(err)
	}
	
	symbol, err := c0.Symbol(nil)
	if err != nil {
		// Try DSToken (MKR et al)
		c1, err0 := NewDSToken(common.HexToAddress(addr), ec)
		if err0 != nil {
			log.Error("NewDSToken", "err", err0)
			panic(err0)
		}
		
		symbol, err1 := c1.Symbol(nil)
		if err1 != nil {
			switch addr {
			case "0xE0B7927c4aF23765Cb51314A0E0521A9645F0E2A":
				return "DGD" // fucking digix
			}
			log.Warn("c1.Symbol()", "err", err1, "addr", addr)
			// fuck it, use first 3 hex digits...
			return addr[:3]
		}
		//log.Info("FFS", "symbol", symbol)
		return strings.Trim(string(symbol[:]), string([]byte{0}))
	}
	return symbol
}
