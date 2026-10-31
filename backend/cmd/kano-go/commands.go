package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/yexca/kano-navi/backend/internal/config"
	"github.com/yexca/kano-navi/backend/internal/dotenv"
	"github.com/yexca/kano-navi/backend/internal/jobs"
	"github.com/yexca/kano-navi/backend/internal/llmadmin"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/workflow"
)

// runCommand runs a one-shot maintenance command against the runtime
// database. Like the service it reads .env, with the environment winning.
func runCommand(command string, args []string) int {
	if err := dotenv.Load(".env"); err != nil {
		return fail("environment file could not be read", 2)
	}
	flags := flag.NewFlagSet("kano-go "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	database := flags.String("database", config.DefaultDatabasePath, "path to the SQLite snapshot")
	overwrite := flags.Bool("overwrite", false, "restore the shipped static records")
	force := flags.Bool("force", false, "replace an already stored provider key")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return fail("invalid arguments for "+command, 2)
	}
	switch command {
	case "seed":
		return runSeed(*database, *overwrite)
	case "sync":
		return runSync(*database)
	default:
		return runMigrateLLM(*database, *force)
	}
}

// runSeed creates or upgrades the database and adds missing static records.
// Source snapshots, events, media and provider configuration are left alone.
func runSeed(database string, overwrite bool) int {
	store, err := sqlite.OpenStandalone(database)
	if err != nil {
		return fail("database unavailable", 1)
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = store.SeedStatic(ctx, overwrite); err != nil {
		return fail("static records could not be seeded", 1)
	}
	counts, err := store.SnapshotCounts(ctx)
	if err != nil {
		return fail("database unavailable", 1)
	}
	encoded, _ := json.Marshal(counts)
	fmt.Println("Seeded " + database)
	fmt.Println(string(encoded))
	return 0
}

// runSync runs every catalog step once through the same executor and
// single-flight reservation the service uses, then exits. A running service
// that is already synchronizing makes this command report a busy queue.
func runSync(database string) int {
	secretsKey := strings.TrimSpace(os.Getenv("LLM_SECRETS_KEY"))
	if secretsKey == "" {
		return fail("LLM_SECRETS_KEY is not configured; refusing to start", 2)
	}
	store, err := sqlite.OpenStandalone(database)
	if err != nil {
		return fail("database unavailable", 1)
	}
	defer store.Close()
	run := newPipeline(store, secretsKey)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	reserved, err := run.executor.PrepareDefinition(ctx, sqlite.WorkflowDefinition{ID: sqlite.AdhocSyncWorkflow, Steps: append([]string{}, jobs.StepOrder...)}, workflow.TriggerOperator, "", nil)
	if errors.Is(err, sqlite.ErrWorkflowBusy) {
		return fail("another synchronization is already running", 1)
	}
	if err != nil {
		return fail("synchronization could not be started", 1)
	}
	_ = store.LabelSyncRun(ctx, reserved.ID, "manual", "cli")
	finished, err := run.executor.ExecuteRun(ctx, reserved)
	if err != nil || finished == nil {
		return fail("synchronization stopped before it finished", 1)
	}
	for _, step := range finished.Steps {
		fmt.Printf("%s: %s\n", step.StepID, step.Status)
	}
	fmt.Println("sync: " + finished.Status)
	if finished.Status != "succeeded" && finished.Status != "partial" {
		return 1
	}
	return 0
}

// runMigrateLLM is the only reader of OPENAI_API_KEY: it seals that value as
// the stored key of the openai-default provider. The service itself never
// reads provider keys from the environment.
func runMigrateLLM(database string, force bool) int {
	sourceKey := os.Getenv("OPENAI_API_KEY")
	secretsKey := os.Getenv("LLM_SECRETS_KEY")
	if sourceKey == "" {
		return fail("OPENAI_API_KEY is required only for this migration", 2)
	}
	if secretsKey == "" {
		return fail("LLM_SECRETS_KEY is required to encrypt the migrated key", 2)
	}
	store, err := sqlite.OpenStandalone(database)
	if err != nil {
		return fail("database unavailable", 1)
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = llmadmin.MigrateProviderKey(ctx, store, llmadmin.LegacyProviderID, sourceKey, secretsKey, force)
	switch {
	case errors.Is(err, llmadmin.ErrMigrationProviderMissing), errors.Is(err, llmadmin.ErrMigrationKeyExists), errors.Is(err, llmadmin.ErrMigrationUnverified):
		return fail(err.Error(), 1)
	case err != nil:
		return fail("provider key could not be stored", 1)
	}
	fmt.Println("Stored the OpenAI provider key in SQLite for " + llmadmin.LegacyProviderID + ". Remove OPENAI_API_KEY from the runtime environment after verification.")
	return 0
}
