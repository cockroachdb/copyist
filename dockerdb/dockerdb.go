// Copyright 2020 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package dockerdb

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// closer implements the io.Closer interface by invoking an arbitrary function
// when Close is called.
type closer func() error

func (c closer) Close() error {
	return c()
}

// Run docker with the given args, then wait for the given database to be
// ready. Start returns an io.Closer interface. The caller must call Close when
// the docker container is no longer needed, and should be terminated. Here is
// an example invocation:
//
//   defer dockerdb.Start(
//     "-p 26257:26257 cockroachdb/cockroach:v20.1.3 start --insecure",
//     "postgres",
//     "postgresql://root@localhost:26257?sslmode=disable",
//   ).Close()
//
func Start(dockerArgs, driverName, dataSourceName string) io.Closer {
	containerName := driverName + "-copyist-testing"

	// Remove any docker containers of this name.
	exec.Command("docker", "rm", containerName, "-f").Run()

	// Start up docker.
	var out bytes.Buffer
	args := strings.Split(dockerArgs, " ")
	args = append([]string{"run", "--name", containerName}, args...)
	cmd := exec.Command("docker", args...)
	cmd.Stderr = &out
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		panic(err)
	}

	// Wait for the database to start. If the docker process exits before the
	// database has started, then something must have gone wrong, so panic with
	// output of the docker process.
	var noPanic int64
	go func() {
		cmd.Wait()
		if atomic.LoadInt64(&noPanic) == 0 {
			panic(out.String())
		}
	}()
	waitForDB(driverName, dataSourceName)

	// Database has successfully started, so don't panic when docker process
	// exits.
	atomic.AddInt64(&noPanic, 1)

	// Remove container
	return closer(func() error {
		exec.Command("docker", "rm", containerName, "-f").Run()
		return nil
	})
}

func waitForDB(driverName, dataSourceName string) {
	db, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// Wait for up to 60 seconds for database to be ready (docker might be
	// downloading image, starting up, etc).
	for i := 0; i < 12; i++ {
		end := time.Now().Add(time.Second * 5)
		for time.Now().Before(end) {
			if db.Ping() == nil {
				return
			}
		}
		log.Printf("waited %d seconds for database to start...", (i+1)*5)
	}

	panic(errors.New("database did not start up within 60 seconds"))
}
