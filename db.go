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
	"math"
	"math/big"
	"time"
	
	"github.com/ethereum/go-ethereum/log"
	
	//"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

var dbPool *pgxpool.Pool

func initDBPool() {
	config, err := pgxpool.ParseConfig(dbConnString)
	if err != nil {
		log.Error("pgxpool.ParseConfig", "err", err)
		panic(err)
	}
	
	hours, _ := time.ParseDuration(pgxMaxConnTime)
	config.MaxConnLifetime = hours
	config.MaxConnIdleTime = hours
	
	config.MaxConns = pgxMaxConns
	
	p, err := pgxpool.ConnectConfig(context.Background(), config)
	if err != nil {
		log.Error("pgxpool.Connect", "err", err)
		panic(err)
	} else {
		dbPool = p
		log.Info("pgxpool.Connect OK")
	}
}

func dbExec(sql string, args []interface{}) {
	//t0 := time.Now()
	_, err := dbPool.Exec(context.Background(), sql, args...)
	if err != nil {
		log.Error("dbConn.Exec", "err", err, "sql", sql, "args", args)
		return
	}
	//t1 := time.Since(t0)
	//log.Info("dbConn.Exec OK", "cmdtag", cmdTag, "t", t1)
}

func dbQueryUint64(sql string, args []interface{}) uint64 {
	t0 := time.Now()
	rows, err := dbPool.Query(context.Background(), sql, args...)
	if err != nil {
		log.Error("dbConn.Query", "err", err)
		panic(err)
	}
	defer rows.Close()
	t1 := time.Since(t0)
	log.Info("dbConn.Query OK", "t", t1)

	// empty table
	if !rows.Next() {
		return 0
	}

	var block uint64
	err = rows.Scan(&block)
	if err != nil {
		log.Error("rows.Scan", "err", err)
		panic(err)
	}
	
	// Any errors encountered by rows.Next or rows.Scan will be returned here
	if rows.Err() != nil {
		log.Error("rows.Err", "err", err)
		panic(err)
	}

	return block
}

type USV2PairCreated struct {
	ticker string
	block uint64
	tx_hash string
	token0, token1, pair_addr string
	pair_id uint64
}
func dbQueryPairsCreated(sql string, args []interface{}) []*USV2PairCreated {
	t0 := time.Now()
	rows, err := dbPool.Query(context.Background(), sql, args...)
	if err != nil {
		log.Error("dbConn.Query", "err", err)
		panic(err)
	}
	defer rows.Close()
	t1 := time.Since(t0)
	log.Info("dbConn.Query OK", "t", t1)

	pairs := []*USV2PairCreated{}
	for rows.Next() {
		var block, pair_id uint64
		var ticker, tx_hash, token0, token1, pair_addr string
		err = rows.Scan(&ticker, &block, &tx_hash, &token0, &token1, &pair_addr, &pair_id)
		if err != nil {
			log.Error("rows.Scan", "err", err)
			panic(err)
		}
		pair := USV2PairCreated{ticker, block, tx_hash, token0, token1, pair_addr, pair_id}
		pairs = append(pairs, &pair)
	}

	// Any errors encountered by rows.Next or rows.Scan will be returned here
	if rows.Err() != nil {
		log.Error("rows.Err", "err", err)
		panic(err)
	}

	return pairs
}

// TODO: this is just for testing; remove when moving to postgresql numeric
func BigToFloat(bi *big.Int) float64 {
	bf := new(big.Float).SetInt(bi)
	f, acc := bf.Float64()
	if acc != big.Exact {
		if f == 0 || f == -0 || f == math.Inf(1) || f == math.Inf(-1) {
			log.Error("big.Float.Float64", "float64", f, "Accuracy", acc)
		} else {
			log.Warn("big.Float.Float64", "float64", f, "Accuracy", acc)
		}
	}
	return f
}
