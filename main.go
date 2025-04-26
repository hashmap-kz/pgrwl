package main

import (
	"context"
	"fmt"
	"log"

	"pgreceivewal5/internal/postgres"
	"pgreceivewal5/internal/xlog"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/wal-g/tracelog"
)

func getCurrentWalInfo() (slot postgres.PhysicalSlot, walSegmentBytes uint64, err error) {
	slotName := "pg_recval_5"

	// Creating a temporary connection to read slot info and wal_segment_size
	tmpConn, err := postgres.Connect()
	if err != nil {
		return
	}
	defer tmpConn.Close(context.TODO())

	queryRunner, err := postgres.NewPgQueryRunner(tmpConn)
	if err != nil {
		return
	}

	slot, err = queryRunner.GetPhysicalSlotInfo(slotName)
	if err != nil {
		return
	}

	walSegmentBytes, err = queryRunner.GetWalSegmentBytes()
	return
}

func main() {
	// 1
	slot, ws, err := getCurrentWalInfo()
	if err != nil {
		log.Fatal(err)
	}
	// TODO: check range
	xlog.WalSegSz = ws

	// 2
	conn, err := pgconn.Connect(context.Background(), "application_name=walg_test_slot user=postgres replication=yes")
	tracelog.ErrorLogger.FatalOnError(err)
	defer conn.Close(context.Background())

	sysident, err := pglogrepl.IdentifySystem(context.Background(), conn)
	tracelog.ErrorLogger.FatalOnError(err)

	if !slot.Exists {
		tracelog.InfoLogger.Println("Trying to create the replication slot")
		_, err = pglogrepl.CreateReplicationSlot(context.Background(), conn, slot.Name, "",
			pglogrepl.CreateReplicationSlotOptions{Mode: pglogrepl.PhysicalReplication})
		tracelog.ErrorLogger.FatalOnError(err)
	}

	fmt.Println(sysident)
}
