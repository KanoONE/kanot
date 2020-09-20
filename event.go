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
	"strings"
	//"math/big"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

type ContractSync interface {
	Name() string
	Contract() (common.Address, uint64, abi.ABI)

	EventName([]common.Hash) string
	LogFields(string) ([]string, []string)

	DBLastInsertedBlock() uint64
	DBInsert(*ethclient.Client, *types.Log, []interface{})
}

// https://uniswap.org/docs/v2/smart-contracts/factory/
// event PairCreated(address indexed token0, address indexed token1, address pair, uint);
type GlueUSV2Factory struct {
	dbTableName string
}

func NewGlueUSV2Factory() *GlueUSV2Factory {
	return &GlueUSV2Factory{"us_factory"}
}

func (s *GlueUSV2Factory) Name() string {
	return "USV2Factory"
}

func (s *GlueUSV2Factory) Contract() (common.Address, uint64, abi.ABI) {
	a := loadABI(uniswapFactoryABI)
	return common.HexToAddress(uniswapFactoryAddr), uniswapFactoryCreateBlock, a
}

func (s *GlueUSV2Factory) EventName(topics []common.Hash) string {
	return "PairCreated"
}

func (s *GlueUSV2Factory) LogFields(eventName string) ([]string, []string) {
	tn := []string{"token0", "token1"}
	dnt := []string{"pair", "address", "arg3", "smalluint"}
	return tn, dnt
}

func (s *GlueUSV2Factory) DBLastInsertedBlock() uint64 {
	q := "SELECT block FROM " + s.dbTableName + " ORDER BY block DESC LIMIT 1"
	return dbQueryUint64(q, []interface{}{})
}

func (s *GlueUSV2Factory) DBInsert(ec *ethclient.Client, l *types.Log, args []interface{}) {
	tokenAddr0, tokenAddr1 := args[2].(string), args[3].(string)
	pairSymbol := getSymbol(ec, tokenAddr0) + "" + getSymbol(ec, tokenAddr1)
	q := "INSERT INTO " + s.dbTableName + " (pair, block, tx_hash, token0, token1, pair_addr, pair_id) VALUES ('" + pairSymbol + "', $1, $2, $3, $4, $5, $6)"
	dbExec(q, args)
}

// https://uniswap.org/docs/v2/smart-contracts/pair/
type GlueUSV2Pair struct {
	contractAddr common.Address
	contractABI abi.ABI
	createBlock uint64
	pairTicker string
	dbTableBase string
}

func NewGlueUSV2Pair(addr common.Address, block uint64, ticker string) *GlueUSV2Pair {
	a := loadABI(uniswapPairABI)
	return &GlueUSV2Pair{
		contractAddr: addr,
		contractABI: a,
		createBlock: block,
		pairTicker: ticker,
		dbTableBase: "us_pair_",
	}
}

func (s *GlueUSV2Pair) Name() string {
	return "USV2Pair_" + s.pairTicker
}

func (s *GlueUSV2Pair) Contract() (common.Address, uint64, abi.ABI) {
	return s.contractAddr, s.createBlock, s.contractABI
}

func (s *GlueUSV2Pair) EventName(topics []common.Hash) string {
	// Sync event is the only unindexed event
	if len(topics) == 0 {
		return "Sync"
	} else {
		// Otherwise the first topic identifies the event
		ev, err := s.contractABI.EventByID(topics[0])
		if err != nil {
			log.Error("contractABI.EventByID", "err", err)
			panic(err)
		}
		return ev.RawName
	}
}

func (s *GlueUSV2Pair) LogFields(eventName string) ([]string, []string) {
	// TODO: refactor this: unnamed args, use ABI types
	tn, dnt := []string{}, []string{}
	
	switch eventName {
	case "Mint":
		tn = append(tn, "sender")
		dnt = append(dnt, "amount0", "biguint", "amount1", "biguint")
	case "Burn":
		tn = append(tn, "sender", "to")
		dnt = append(dnt, "amount0", "biguint", "amount1", "biguint")
	case "Swap":
		tn = append(tn, "sender", "to")
		dnt = append(dnt,
			"amount0In", "biguint", "amount1In", "biguint",
			"amount0Out", "biguint", "amount1Out", "biguint")
	case "Sync":
		dnt = append(dnt, "reserve0", "biguint", "reserve1", "biguint")
	case "Approval":
		tn = append(tn, "owner", "spender")
		dnt = append(dnt, "value", "biguint")
	case "Transfer":
		tn = append(tn, "from", "to")
		dnt = append(dnt, "value", "biguint")		
	}

	return tn, dnt
}

func (s *GlueUSV2Pair) DBLastInsertedBlock() uint64 {
	getBlock := func(eventName string) uint64 {
		q := "SELECT block FROM " + s.dbTableBase + eventName +
			" WHERE pair = '" + s.pairTicker + "' ORDER BY block DESC LIMIT 1"
		return dbQueryUint64(q, []interface{}{})
	}

	blocks := []uint64{
		getBlock("mint"),
		getBlock("burn"),
		getBlock("swap"),
		getBlock("sync"),
		getBlock("approval"),
		getBlock("transfer"),
	}
	var last uint64
	for _, b := range blocks {
		if b > last {
			last = b
		}
	}
	return last
}

func (s *GlueUSV2Pair) DBInsert(ec *ethclient.Client, l *types.Log, args []interface{}) {
	eventName := s.EventName(l.Topics)
	insertQuery := "INSERT INTO " + s.dbTableBase + eventName + " (pair, block, tx_hash, "
	
	switch eventName {
	case "Mint":
		insertQuery += ("sender, amount0, amount1) VALUES ('"+ s.pairTicker + "', $1, $2, $3, $4, $5)")
	case "Burn":
		insertQuery += ("sender, dest, amount0, amount1) VALUES ('" + s.pairTicker + "', $1, $2, $3, $4, $5, $6)")
	case "Swap":
		insertQuery += ("sender, dest, amount0In, amount1In, amount0Out, amount1Out) VALUES ('" + s.pairTicker + "', $1, $2, $3, $4, $5, $6, $7, $8)")
	case "Sync":
		insertQuery += ("reserve0, reserve1) VALUES ('" + s.pairTicker + "', $1, $2, $3, $4)")
	case "Approval":
		insertQuery += ("owner, spender, value) VALUES ('" + s.pairTicker + "', $1, $2, $3, $4, $5)")
	case "Transfer":
		insertQuery += ("sender, dest, value) VALUES ('" + s.pairTicker + "', $1, $2, $3, $4, $5)")
	}

	dbExec(insertQuery, args)
}


func loadABI(s string) abi.ABI {
	a, err := abi.JSON(strings.NewReader(s))
	if err != nil {
		log.Error("abi.JSON", "err", err)
		panic(err)
	}
	return a
}
