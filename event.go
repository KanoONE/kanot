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



type EventSync interface {	
	ContractAddr() common.Address

	ContractCreateBlock() uint64

	ContractABI() abi.ABI

	EventName() string

	LogTopicNames() []string

	LogDataNamesAndTypes() []string

	DBTableName() string

	DBInsertQuery() string

	// TODO: "post proc/hook"
}

type UniswapFactory struct {
}

func (f *UniswapFactory) ContractAddr() common.Address {
	return common.HexToAddress(uniswapFactoryAddr)
}

func (f *UniswapFactory) ContractCreateBlock() uint64 {
	return uniswapFactoryCreateBlock
}

func (f *UniswapFactory) ContractABI() abi.ABI {
	a, err := abi.JSON(strings.NewReader(uniswapFactoryABI))
	if err != nil {
		log.Error("abi.JSON", "err", err)
		panic(err)
	}
	return a
}

func (f *UniswapFactory) EventName() string {
	return "PairCreated"
}

// https://uniswap.org/docs/v2/smart-contracts/factory/
// event PairCreated(address indexed token0, address indexed token1, address pair, uint);
func (f *UniswapFactory) LogTopicNames() []string {
	return []string{"token0", "token1"}
}

// TODO: refactor this, especially the unnamed args
func (f *UniswapFactory) LogDataNamesAndTypes() []string {
	return []string{"pair", "address", "arg3", "uint"}
}

func (f *UniswapFactory) DBTableName() string {
	return "us_factory"
}

func (f *UniswapFactory) DBInsertQuery() string {
	return "INSERT INTO " + f.DBTableName() + " (token0, token1, pair_addr, pair_id) VALUES ($1, $2, $3, $4)"
}
