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
)

type EventSync struct {	
	ContractAddr common.Address
	ContractCreateBlock uint64
	ContractABI *abi.ABI
	
	EventName string
	LogTopicNames []string
	LogDataNamesAndTypes []string

	DBTableName string
	DBBlockQuery string
	DBInsertQuery string
}

// https://uniswap.org/docs/v2/smart-contracts/factory/
// event PairCreated(address indexed token0, address indexed token1, address pair, uint);
func UniswapV2FactoryPairCreated() *EventSync {
	a := loadABI(uniswapFactoryABI)
	dbTableName := "us_factory"
	return &EventSync{
		ContractAddr: common.HexToAddress(uniswapFactoryAddr),
		ContractCreateBlock: uniswapFactoryCreateBlock,
		ContractABI: a,
		EventName: "PairCreated",
		LogTopicNames: []string{"token0", "token1"},
		// TODO: refactor this: unnamed args, use ABI types
		LogDataNamesAndTypes: []string{"pair", "address", "arg3", "smalluint"},
		DBTableName: dbTableName,
		DBBlockQuery: "SELECT block FROM " + dbTableName + " ORDER BY block DESC LIMIT 1",
		DBInsertQuery: "INSERT INTO " + dbTableName + " (block, token0, token1, pair_addr, pair_id) VALUES ($1, $2, $3, $4, $5)",
	}
}

// https://uniswap.org/docs/v2/smart-contracts/pair/
func UniswapV2Pair(pairTicker string, addr common.Address, createBlock uint64, eventName string) *EventSync {
	a := loadABI(uniswapPairABI)

	dbTableName := "us_pair_" + eventName
	insertQuery := "INSERT INTO " + dbTableName + " "

	// TODO: refactor this: unnamed args, use ABI types
	tn, dnt := []string{}, []string{}
	
	switch eventName {
	case "Mint":
		tn = append(tn, "sender")
		dnt = append(dnt, "amount0", "uint", "amount1", "uint")
		insertQuery += "(block, pair, sender, amount0, amount1) VALUES (" + pairTicker + ", $1, $2, $3, $4)"
	case "Burn":
		tn = append(tn, "sender", "to")
		dnt = append(dnt, "amount0", "uint", "amount1", "uint")
		insertQuery += "(block, pair, sender, to, amount0, amount1) VALUES (" + pairTicker + ", $1, $2, $3, $4, $5)"
	case "Swap":
		tn = append(tn, "sender", "to")
		dnt = append(dnt,
			"amount0In", "uint", "amount1In", "uint",
			"amount0Out", "uint", "amount1Out", "uint")
		insertQuery += "(block, pair, sender, to, amount0In, amount1In, amount0Out, amount1Out) VALUES (" + pairTicker + ", $1, $2, $3, $4, $5, $6, $7)"
	case "Sync":
		dnt = append(dnt, "reserve0", "uint", "reserve1", "uint")
		insertQuery += "(block, pair, reserve0, reserve1) VALUES (" + pairTicker + ", $1, $2, $3)"
	}

	return &EventSync{
		ContractAddr: addr,
		ContractCreateBlock: createBlock,
		ContractABI: a,
		EventName: eventName,
		LogTopicNames: tn,
		LogDataNamesAndTypes: dnt,
		DBTableName: dbTableName,
		DBBlockQuery: "SELECT block FROM " + dbTableName + " WHERE pair = " + pairTicker + " ORDER BY block DESC LIMIT 1",
		DBInsertQuery: insertQuery,
	}
}

func loadABI(s string) *abi.ABI {
	a, err := abi.JSON(strings.NewReader(s))
	if err != nil {
		log.Error("abi.JSON", "err", err)
		panic(err)
	}
	return &a
}
