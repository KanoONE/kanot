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

func setupDBConn() *pgxpool.Conn {
	config, err := pgxpool.ParseConfig(dbConnString)
	if err != nil {
		log.Error("pgxpool.ParseConfig", "err", err)
		panic(err)
	}
	
	hours, _ := time.ParseDuration(pgxMaxConnTime)
	config.MaxConnLifetime = hours
	config.MaxConnIdleTime = hours
	
	config.MaxConns = pgxMaxConns
	
	dbPool, err := pgxpool.ConnectConfig(context.Background(), config)
	if err != nil {
		log.Error("pgxpool.Connect", "err", err)
		panic(err)
	} else {
		log.Info("pgxpool.Connect OK")
	}
	
	dbConn, err := dbPool.Acquire(context.Background())
	if err != nil {
		log.Error("pgxpool.Pool.Acquire", "err", err)
		panic(err)
	}

	return dbConn
}

type DBExec struct {
	sql string
	args []interface{}
}

func dbHandler(dbConn *pgxpool.Conn, ch <-chan DBExec) {
	for e := range ch {
		t0 := time.Now()
		cmdTag, err := dbConn.Exec(context.Background(), e.sql, e.args...)
		if err != nil {
			log.Error("dbConn.Exec", "err", err)
		} else {
			t1 := time.Since(t0)
			log.Info("dbConn.Exec OK", "cmdtag", cmdTag, "t", t1)
		}
	}
	
	dbConn.Release()
}

func insertTransfer(dbConn *pgxpool.Conn, pair UniswapPair, ts uint64, tx_hash, from, to string, value *big.Int) {
	//log.Info("insertTransfer", "dbConn.IsClosed()", dbConn.IsClosed())
	table := pair.DBTableName("transfer")
	q := "INSERT INTO " + table + " (timestamp, tx_hash, from_addr, to_addr, value) VALUES ($1, $2, $3, $4, $5)"
	cmdTag, err := dbConn.Exec(context.Background(), q, ts, tx_hash, from, to, BigToFloat(value))
	if err != nil {
		log.Error("dbConn.Query", "err", err)
		panic(err)
	} else {
		log.Info("query OK", "table", table, "cmdtag", cmdTag)
	}
}

func insertSwap(dbConn *pgxpool.Conn, pair UniswapPair, ts uint64, tx_hash, from, to string, a0In, a1In, a0Out, a1Out *big.Int) {
	table := pair.DBTableName("swap")
	q := "INSERT INTO " + table + " (timestamp, tx_hash, from_addr, to_addr, amount_0_in, amount_1_in, amount_0_out, amount_1_out) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)"
	cmdTag, err := dbConn.Exec(context.Background(), q, ts, tx_hash, from, to, BigToFloat(a0In), BigToFloat(a1In), BigToFloat(a0Out), BigToFloat(a1Out))
	if err != nil {
		log.Error("dbConn.Query", "err", err)
		panic(err)
	} else {
		log.Info("query OK", "table", table, "cmdtag", cmdTag)
	}
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
