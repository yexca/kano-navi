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
	encoded, err := render(*databasePath, *days, *nowValue)
	if err != nil {
		switch err.Error() {
		case "invalid timestamp":
			fmt.Fprintln(os.Stderr, "kano-parity: invalid -now")
			os.Exit(2)
		case "database unavailable":
			fmt.Fprintln(os.Stderr, "kano-parity: database unavailable")
			os.Exit(1)
		case "dashboard unavailable":
			fmt.Fprintln(os.Stderr, "kano-parity: dashboard unavailable")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "kano-parity: projection unavailable")
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(encoded)
}

func render(databasePath string, days int, nowValue string) ([]byte, error) {
	now, err := time.Parse(time.RFC3339Nano, nowValue)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp")
	}
	store, err := sqlite.OpenReadOnly(databasePath)
	if err != nil {
		return nil, fmt.Errorf("database unavailable")
	}
	defer store.Close()
	dashboard, err := store.GetDashboard(context.Background(), days, now)
	if err != nil {
		return nil, fmt.Errorf("dashboard unavailable")
	}
	encoded, err := json.Marshal(publicview.Dashboard(dashboard))
	if err != nil {
		return nil, fmt.Errorf("projection unavailable")
	}
	return encoded, nil
}
