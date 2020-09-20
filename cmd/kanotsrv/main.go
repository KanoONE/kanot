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

package main

import (
	"os"
	"os/signal"

	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli"

	"github.com/KanoONE/kanot"
)

func init() {
	kanot.InitLog()
}

func main() {
	app := cli.NewApp()
	app.Name = "kanot"
	app.Version = "0.0.2-unstable"
	app.Usage = ""

	app.Flags = []cli.Flag{
	}

	app.Action = func(c *cli.Context) error {
		sigs := make(chan os.Signal, 1)
		done := make(chan bool, 1)
	
		signal.Notify(sigs, os.Interrupt)
	
		go func() {
			<-sigs
			done <- true
		}()
		
		log.Info("Kano Terminal Server", "version", app.Version)
		kanot.SyncETH()
		
		<-done
		log.Info("shutting down...")
		// TODO: close DB conns etc

		return nil
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Error("app.Run:", "err", err)
	}
}
