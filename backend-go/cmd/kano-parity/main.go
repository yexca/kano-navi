// Command kano-parity renders a fixed-time public dashboard projection for
// the TypeScript/Go parity script. It is a local test utility, not a service
// entry point and it never opens a database for writing.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/publicview"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

func main() {
	databasePath := flag.String("database", "", "existing SQLite snapshot")
	days := flag.Int("days", 3, "dashboard post window")
	nowValue := flag.String("now", "", "fixed RFC3339 timestamp")
	flag.Parse()
	if *databasePath == "" || *nowValue == "" {
		fmt.Fprintln(os.Stderr, "kano-parity: -database and -now are required")
		os.Exit(2)
	}
	now, err := time.Parse(time.RFC3339Nano, *nowValue)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kano-parity: invalid -now")
		os.Exit(2)
	}
	store, err := sqlite.OpenReadOnly(*databasePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kano-parity: database unavailable")
		os.Exit(1)
	}
	defer store.Close()
	dashboard, err := store.GetDashboard(context.Background(), *days, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kano-parity: dashboard unavailable")
		os.Exit(1)
	}
	encoded, err := json.Marshal(publicview.Dashboard(dashboard))
	if err != nil {
		fmt.Fprintln(os.Stderr, "kano-parity: projection unavailable")
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(encoded)
}
