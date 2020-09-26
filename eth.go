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
	//"math"
	"math/big"
	//"encoding/hex"
	"strings"
	"os"
	//"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/jackc/pgx/v4/pgxpool"

	"net/http"
	_ "net/http/pprof"
)

const (
	//
	// Ethereum
	//
	// websocket endpoint of Ethereum full node
	endpoint = "ws://127.0.0.1:13516"

	// number of blocks for high guarantee of no reorgs
	blockConfirmations = 15

	//
	// PostgreSQL
	//
	dbConnString = "host=127.0.0.1 port=5432 dbname=dev1 user=kanot password=kanot"

	// used for both pgxpool.Config.MaxConnLifetime and pgxpool.Config.MaxConnIdleTime
	// TODO: for now set high so conns are open indefinitely
	pgxMaxConnTime = "876000h" // 100 years

	pgxMaxConns = 6

	//
	// Performance Tuning
	//
	pollingCycle = 300 * time.Second
	queryBlockCount = 32
	queryTimeout = 240 * time.Second
	syncWorkers = 1
)

func InitLog() {
	//log.StreamHandler(os.Stderr, log.TerminalFormat(true)),
	log.Root().SetHandler(log.MultiHandler(
		//log.MatchFilterHandler("pkg", "app/kanot", log.StdoutHandler),
		log.LvlFilterHandler(log.LvlInfo, log.StreamHandler(os.Stderr, log.TerminalFormat(true)))))
}

func SyncETH() {
	go func() {
		err := http.ListenAndServe("localhost:6060", nil)
		if err != nil {
			log.Error("http.ListenAndServe", "err", err)
		}
	}()

	initDBPool()

	syncUniswap()
}

func syncUniswap() {
	ec := getETHClient()
	dbConn := getDBConn()
	usf := NewGlueUSV2Factory()
	usfAddr, usfCreateBlock, _ := usf.Contract()

	addrs := []common.Address{usfAddr}
	csm := make(map[common.Address]ContractSync)
	csm[usfAddr] = usf

	q := "SELECT * FROM us_factory ORDER BY block DESC"
	pairs := dbQueryPairsCreated(dbConn, q, []interface{}{})

	for _, p := range pairs {
		addr := common.HexToAddress(p.pair_addr)
		addrs = append(addrs, addr)
		cs := NewGlueUSV2Pair(addr, p.block, p.ticker)
		csm[addr] = cs
	}

	fromBlock := usfCreateBlock
	if len(pairs) > 0 {
		fromBlock = pairs[0].block
	}

	headBlock, _ := getHeadBlockAndTime(ec)
	maxBlock := headBlock - blockConfirmations

	if fromBlock > maxBlock {
		log.Info("up-to-date before sync", "fromBlock", fromBlock, "headBlock", headBlock, "pairs", len(pairs))
		return
	}

	log.Info("syncing", "fromBlock", fromBlock, "maxBlock", maxBlock, "pairs", len(pairs))

	getLogs := func(fb, tb uint64, as []common.Address) ([]types.Log, time.Duration) {
		fq := ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(fb),
			ToBlock: new(big.Int).SetUint64(tb),
			Addresses: as}
		t0 := time.Now()
		logs, err := ec.FilterLogs(context.Background(), fq)
		if err != nil {
			log.Error("ethclient.FilterLogs", "err", err)
			panic(err)
		}
		return logs, time.Since(t0)
	}

	var toBlock uint64
	for {
		toBlock = fromBlock + queryBlockCount
		if toBlock > maxBlock {
			toBlock = maxBlock
		}

		logs, t1 := getLogs(fromBlock, toBlock, addrs)
		t2 := time.Now()
		fLogs := []types.Log{}
		for _, l := range logs {
			if l.Address == usfAddr {
				fLogs = append(fLogs, l)
			} else {
				// Insert all pair logs
				cs := csm[l.Address]
				args := parseLog(l, cs)
				cs.Insert(dbConn, ec, l, args)
			}
		}
		t3 := time.Since(t2)

		log.Info("sync", "fromBlock", fromBlock, "left", maxBlock-fromBlock, "addrs", len(addrs), "logs", len(logs), "fl", t1, "in", t3)

		// If we have factory logs, parse out new pair addresses,
		// get their logs and insert them.
		if len(fLogs) > 0 {
			npAddrs := []common.Address{}
			for _, fl := range fLogs {
				args := parseLog(fl, usf)
				t0, t1, a := args[2].(string), args[3].(string), args[4].(string)
				pa := common.HexToAddress(a)
				ticker := getTicker(dbConn, ec, t0, t1)
				cs := NewGlueUSV2Pair(pa, fl.BlockNumber, ticker)
				csm[pa] = cs
				npAddrs = append(npAddrs, pa)
			}
			addrs = append(addrs, npAddrs...)

			pLogs, t4 := getLogs(fromBlock, toBlock, npAddrs)
			t5 := time.Now()
			for _, l := range pLogs {
				cs := csm[l.Address]
				args := parseLog(l, cs)
				cs.Insert(dbConn, ec, l, args)
			}
			t6 := time.Since(t5)

			log.Info("re-sync new pairs", "fromBlock", fromBlock, "newPairs", len(npAddrs), "fl", t4, "in", t6)
			// Insert factory logs last, so that if committed to DB we
			// know that all pair logs in the block range are also committed.
			// This can be safely used to initialize the address set
			// on arbitrary sync restarts.
			for _, l := range fLogs {
				args := parseLog(l, usf)
				usf.Insert(dbConn, ec, l, args)
			}
		}

		fromBlock = fromBlock + queryBlockCount + 1
		if fromBlock > maxBlock {
			log.Info("up-to-date after sync", "fromBlock", fromBlock, "headBlock", headBlock)
			return
		}
	}
}

func parseLog(l types.Log, cs ContractSync) []interface{} {
	_, _, cABI := cs.Contract()
	eventName := cs.EventName(l.Topics)
	tn, dnt := cs.LogFields(eventName)

	args := make([]interface{}, 0)
	args = append(args, l.BlockNumber, l.TxHash.Hex())

	for i, _ := range tn {
		addr := common.HexToAddress(l.Topics[i+1].Hex())
		args = append(args, addr.Hex())
	}

	m := make(map[string]interface{})
	err := cABI.UnpackIntoMap(m, eventName, l.Data)
	if err != nil {
		log.Error("ABI.Unpack", "err", err, "en", eventName, "l", l, "abi", cABI)
		panic(err)
	}

	// TODO: use types / direct decoding from ABI
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
			args = append(args, b.String())
		}
		i += 2
	}

	return args
}

func getDBConn() *pgxpool.Conn {
	dbConn, err := dbPool.Acquire(context.Background())
	if err != nil {
		log.Error("dbPool.Acquire", "err", err)
		panic(err)
	}
	return dbConn
}

func getETHClient() *ethclient.Client {
	c, err := ethclient.Dial(endpoint)
	if err != nil {
     	log.Error("rpc.Dial", "err", err)
		panic(err)
	}
	return c
}

func getHeadBlockAndTime(c *ethclient.Client) (uint64, time.Time) {
	lastHeader, err := c.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Error("client.HeaderByNumber", "err", err)
		panic(err)
	}

	headBlock := lastHeader.Number.Uint64()
	t := time.Unix(int64(lastHeader.Time), 0)
	return headBlock, t
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
