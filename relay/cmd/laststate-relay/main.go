// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.bug.st/serial"

	"github.com/laststate/relay/internal/adapter"
	"github.com/laststate/relay/internal/admin"
	"github.com/laststate/relay/internal/analysis"
	"github.com/laststate/relay/internal/artifact"
	"github.com/laststate/relay/internal/bundle"
	"github.com/laststate/relay/internal/config"
	"github.com/laststate/relay/internal/delivery"
	"github.com/laststate/relay/internal/framing"
	"github.com/laststate/relay/internal/ingest"
	"github.com/laststate/relay/internal/latchstream"
	"github.com/laststate/relay/internal/lep"
	"github.com/laststate/relay/internal/match"
	"github.com/laststate/relay/internal/metrics"
	"github.com/laststate/relay/internal/privacy"
	"github.com/laststate/relay/internal/secret"
	mqttsource "github.com/laststate/relay/internal/source"
	"github.com/laststate/relay/internal/store"
	"github.com/laststate/relay/internal/symbolicate"
	"github.com/laststate/relay/internal/symbolserver"
	"github.com/laststate/relay/internal/ui"
)

var (
	cliVersion    = "dev"
	gitCommit     = "unknown"
	buildDate     = "unknown"
	requestLogger = slog.New(slog.NewTextHandler(os.Stderr, nil))
)

func main() {
	args, colorOverride := splitGlobalFlags(os.Args[1:])
	if colorOverride != nil {
		ui.ForceColor.Store(colorOverride)
	}
	if len(args) == 0 {
		if err := infoCmd(nil); err != nil {
			ui.Error(os.Stderr, err.Error())
			os.Exit(1)
		}
		return
	}
	var err error
	switch args[0] {
	case "info":
		err = infoCmd(args[1:])
	case "inspect":
		err = inspectCmd(args[1:])
	case "analyze":
		err = analyzeCmd(args[1:])
	case "import":
		err = importCmd(args[1:])
	case "collect":
		err = collectCmd(args[1:])
	case "run":
		err = runCmd(args[1:])
	case "status":
		err = statusCmd(args[1:])
	case "export":
		err = exportCmd(args[1:])
	case "replay":
		err = replayCmd(args[1:])
	case "doctor":
		err = doctorCmd(args[1:])
	case "artifacts":
		err = artifactsCmd(args[1:])
	case "config":
		err = configCmd(args[1:])
	case "bundle":
		err = bundleCmd(args[1:])
	case "spool":
		err = spoolCmd(args[1:])
	case "destinations":
		err = destinationsCmd(args[1:])
	case "backup":
		err = backupCmd(args[1:])
	case "restore":
		err = restoreCmd(args[1:])
	case "version":
		printVersion()
		return
	case "help", "-h", "--help":
		usage()
		return
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		ui.Error(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func splitGlobalFlags(in []string) ([]string, *bool) {
	var off, on bool
	out := make([]string, 0, len(in))
	for _, value := range in {
		switch value {
		case "--no-color":
			off = true
		case "--color":
			on = true
		default:
			out = append(out, value)
		}
	}
	if off {
		v := false
		return out, &v
	}
	if on {
		v := true
		return out, &v
	}
	return out, nil
}

func usage() {
	fmt.Fprintln(os.Stderr, ui.Banner())
	fmt.Fprintln(os.Stderr, ui.Section("Usage"))
	fmt.Fprintln(os.Stderr, "  laststate-relay [--no-color|--color] <command> [flags]")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, ui.Section("Commands"))
	commands := [][2]string{
		{"run", "start the offline-first relay daemon"},
		{"collect", "collect Latch frames from one serial port"},
		{"info", "show instance overview and probe local endpoints"},
		{"inspect", "validate and inspect a LEP envelope"},
		{"analyze", "decode a crash; auto-match ELF or use --elf / --config"},
		{"import / export", "move events into or out of the durable spool"},
		{"status / doctor", "inspect health, disk pressure and consistency"},
		{"replay", "requeue failed or skipped deliveries"},
		{"artifacts", "add, list, inspect and verify firmware artifacts"},
		{"destinations", "list, pause or resume destinations"},
		{"bundle", "export/import .lsbundle archives (optional Ed25519 signatures)"},
		{"spool", "prune or reconcile the local spool"},
		{"backup / restore", "zip the data directory (stop the daemon first)"},
		{"config", "validate or print normalized configuration"},
		{"version", "print build metadata"},
	}
	table := ui.NewTable(ui.Column{Title: "command", Width: 20}, ui.Column{Title: "description"})
	for _, command := range commands {
		table.AppendRow(command[0], command[1])
	}
	fmt.Fprintln(os.Stderr, table.String())
}

func printVersion() {
	payload := map[string]string{
		"version": cliVersion, "commit": gitCommit, "built": buildDate,
		"go": runtime.Version(), "platform": runtime.GOOS + "/" + runtime.GOARCH,
	}
	if !ui.IsTTY() {
		_ = json.NewEncoder(os.Stdout).Encode(payload)
		return
	}
	fmt.Fprintln(os.Stdout, ui.ShortBanner())
	fmt.Fprintln(os.Stdout, ui.KVTable([][2]string{
		{"version", cliVersion}, {"commit", gitCommit}, {"built", buildDate},
		{"runtime", runtime.Version()}, {"platform", runtime.GOOS + "/" + runtime.GOARCH},
	}))
}

func inspectCmd(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "emit JSON only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: inspect [--json] event.lep")
	}
	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	envelope, err := lep.Validate(raw)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(envelope)
	}
	fmt.Fprintln(os.Stdout, ui.Section("LEP envelope"))
	fmt.Fprintln(os.Stdout, ui.KVTable([][2]string{
		{"version", fmt.Sprintf("%d", envelope.Version)},
		{"event id", fmt.Sprintf("%d", envelope.EventID)},
		{"sequence", fmt.Sprintf("%d", envelope.Sequence)},
		{"type", fmt.Sprintf("%d", envelope.Type)},
		{"architecture", fmt.Sprintf("%d", envelope.Architecture)},
		{"flags", fmt.Sprintf("0x%02x", envelope.Flags)},
		{"payload", fmt.Sprintf("%d bytes", envelope.PayloadLength)},
		{"CRC", "CRC-32/IEEE (0x04C11DB7)"},
	}))
	return nil
}

func analyzeCmd(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "emit JSON only")
	elfPath := fs.String("elf", "", "ELF file containing DWARF symbols")
	dataDir := fs.String("data-dir", "data", "artifact catalog for auto-match")
	configPath := fs.String("config", "", "optional relay.yaml for analysis/crypto settings")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: analyze [--json] [--elf firmware.elf] [--data-dir DIR] [--config relay.yaml] event.lep")
	}
	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}

	var cfg config.Config
	artDir := filepath.Join(*dataDir, "artifacts")
	if *configPath != "" {
		cfg, err = config.Load(*configPath)
		if err != nil {
			return err
		}
		if cfg.Instance.DataDir != "" {
			*dataDir = cfg.Instance.DataDir
		}
		if cfg.Artifacts.Directory != "" {
			artDir = cfg.Artifacts.Directory
		} else {
			artDir = filepath.Join(*dataDir, "artifacts")
		}
	}

	keyring, err := loadCryptoKeyring(cfg)
	if err != nil {
		return fmt.Errorf("crypto: %w", err)
	}
	opts := analysisOptionsFromConfig(cfg, *dataDir)
	opts.Keyring = keyring

	artifactPath := *elfPath
	var matchWarnings []string
	if artifactPath == "" {
		catalog, listErr := artifact.ListFrom(artDir)
		if listErr == nil {
			result, matchErr := match.ResolveFromEvent(raw, catalog, keyring)
			if matchErr == nil {
				matchWarnings = result.Warnings
				if result.Artifact != nil {
					artifactPath = result.Artifact.Path
					opts.ArtifactSHA = result.Artifact.SHA256
				}
			}
		}
		// Optional symbol server fetch when configured and still unmatched.
		if artifactPath == "" && cfg.Analysis.SymbolServer != "" {
			identity, idErr := match.ExtractIdentity(raw, keyring)
			if idErr == nil && identity.BuildID != "" {
				token, _ := secret.Resolve(cfg.Analysis.SymbolServerToken)
				client := symbolserver.Client{BaseURL: cfg.Analysis.SymbolServer, Token: token}
				if item, fetchErr := client.FetchBuildID(context.Background(), artDir, identity.BuildID); fetchErr != nil {
					matchWarnings = append(matchWarnings, "symbol server: "+fetchErr.Error())
				} else {
					artifactPath = item.Path
					opts.ArtifactSHA = item.SHA256
					matchWarnings = append(matchWarnings, "artifact fetched from symbol server")
				}
			}
		}
	}
	opts.ArtifactPath = artifactPath

	report, err := analysis.AnalyzeWithOptions(raw, opts)
	if err != nil {
		return err
	}
	// External symbolizer fallback when DWARF produced no names.
	if artifactPath != "" && needsExternalSymbolizer(report) {
		if frames, extErr := tryExternalSymbolizer(cfg, artifactPath, report); extErr != nil {
			report.Warnings = append(report.Warnings, "external symbolizer: "+extErr.Error())
		} else if len(frames) > 0 {
			report.Frames = frames
		}
	}
	report.Warnings = append(matchWarnings, report.Warnings...)
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	fmt.Fprintln(os.Stdout, ui.Section("LEP analysis"))
	fmt.Fprint(os.Stdout, report.Text())
	return nil
}

// loadCryptoKeyring resolves cfg.crypto keys. Returns nil when crypto is disabled.
func loadCryptoKeyring(cfg config.Config) (lep.Keyring, error) {
	if !cfg.Crypto.Enabled {
		return nil, nil
	}
	if len(cfg.Crypto.Keys) == 0 {
		return nil, fmt.Errorf("crypto.enabled requires at least one key")
	}
	entries := make([]lep.Key, 0, len(cfg.Crypto.Keys))
	for _, item := range cfg.Crypto.Keys {
		ref, err := secret.Resolve(item.Key)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", item.ID, err)
		}
		material, err := lep.ParseKeyMaterial(ref)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", item.ID, err)
		}
		numeric := lep.ParseNumericKeyID(item.ID)
		entries = append(entries, lep.Key{
			NumericID: numeric,
			ID:        lep.KeyIDFromNumeric(numeric),
			Key:       material,
		})
	}
	return lep.BuildMemoryKeyring(entries, cfg.Crypto.ActiveKeyID, cfg.Crypto.AuthKeyID)
}

func analysisOptionsFromConfig(cfg config.Config, dataDir string) analysis.AnalyzeOptions {
	cacheDir := cfg.Analysis.SymbolCacheDir
	if cacheDir == "" {
		cacheDir = filepath.Join(dataDir, "symbol-cache")
	}
	maps := make([]symbolicate.PathRewrite, 0, len(cfg.Analysis.SourcePathMaps))
	for _, item := range cfg.Analysis.SourcePathMaps {
		maps = append(maps, symbolicate.PathRewrite{From: item.From, To: item.To})
	}
	regions := make([]analysis.MemoryRegion, 0, len(cfg.Analysis.MemoryMap))
	for _, item := range cfg.Analysis.MemoryMap {
		regions = append(regions, analysis.MemoryRegion{Name: item.Name, Start: item.Start, End: item.End, Class: item.Class})
	}
	return analysis.AnalyzeOptions{
		MemoryMap: regions,
		PathMap:   symbolicate.PathMap{Prefix: cfg.Analysis.SourcePathPrefix, Maps: maps},
		Cache:     &symbolicate.Cache{Dir: cacheDir},
	}
}

func needsExternalSymbolizer(report analysis.Report) bool {
	if len(report.Frames) == 0 {
		return report.CPU != nil
	}
	for _, frame := range report.Frames {
		if frame.Function != "" || frame.File != "" {
			return false
		}
	}
	return true
}

func tryExternalSymbolizer(cfg config.Config, artifactPath string, report analysis.Report) ([]symbolicate.Frame, error) {
	if report.CPU == nil {
		return nil, nil
	}
	addresses := []uint64{uint64(report.CPU.PC &^ 1)}
	if report.CPU.LR != 0 {
		addresses = append(addresses, uint64(report.CPU.LR&^1))
	}
	binaryPath := cfg.Analysis.LLVMSymbolizer
	style := "llvm"
	switch report.Architecture {
	case analysis.ArchRISCV, analysis.ArchRISCV64:
		if cfg.Analysis.RISCVAddr2Line != "" {
			binaryPath = cfg.Analysis.RISCVAddr2Line
			style = "addr2line"
		}
	case analysis.ArchCortexM:
		if cfg.Analysis.ARMAddr2Line != "" && binaryPath == "" {
			binaryPath = cfg.Analysis.ARMAddr2Line
			style = "addr2line"
		}
	}
	if binaryPath == "" {
		return nil, fmt.Errorf("no external symbolizer configured")
	}
	return symbolicate.ResolveExternal(symbolicate.ExternalConfig{Binary: binaryPath, Style: style}, artifactPath, addresses)
}

func importCmd(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "data", "relay data directory")
	sourceID := fs.String("source", "import", "source identifier")
	jsonOut := fs.Bool("json", false, "emit JSON only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: import [--data-dir DIR] [--source ID] file.lep")
	}
	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	relay, err := dataStore(*dataDir)
	if err != nil {
		return err
	}
	defer relay.Close()
	result, err := (ingest.Service{Store: relay}).Accept(context.Background(), *sourceID, raw)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	ui.Success(os.Stdout, resultText(result.Duplicate)+" · "+result.Event.ID)
	return nil
}

func collectCmd(args []string) error {
	fs := flag.NewFlagSet("collect", flag.ContinueOnError)
	device := fs.String("serial", "", "serial port")
	baud := fs.Int("baud", 115200, "baud rate")
	dataDir := fs.String("data-dir", "data", "relay data directory")
	framingType := fs.String("framing", "latch-stream", "latch-stream or cobs")
	ackMode := fs.String("ack", "none", "none or lsak-v1")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *device == "" {
		return errors.New("--serial is required")
	}
	if *ackMode != "none" && *ackMode != "lsak-v1" {
		return errors.New("--ack must be none or lsak-v1")
	}
	relay, err := dataStore(*dataDir)
	if err != nil {
		return err
	}
	defer relay.Close()
	port, err := serial.Open(*device, &serial.Mode{BaudRate: *baud})
	if err != nil {
		return fmt.Errorf("open serial: %w", err)
	}
	_ = port.SetReadTimeout(time.Second)
	defer port.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	dashboard := ui.NewDashboard(ui.DashboardOptions{Title: "LAST STATE RELAY · collect", Instance: fmt.Sprintf("%s @ %d", *device, *baud), MaxLog: 200})
	publish := dashboard.Publisher()
	service := ingest.Service{Store: relay, Hooks: ingest.Hooks{
		OnAccept: func(source string, result store.Result) {
			kind := ui.EventStored
			if result.Duplicate {
				kind = ui.EventDuplicate
			}
			publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: kind, Source: source, Message: resultText(result.Duplicate), Detail: result.Event.ID}})
		},
		OnError: func(source string, err error) {
			publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventError, Source: source, Message: "rejected", Detail: err.Error()}})
		},
	}}
	dashboardDone := make(chan error, 1)
	go func() { dashboardDone <- dashboard.Run(ctx); cancel() }()
	receiveDone := make(chan error, 1)
	go func() { receiveDone <- receive(ctx, port, port, "serial", *framingType, *ackMode, service) }()
	select {
	case err := <-dashboardDone:
		_ = port.Close()
		return err
	case err := <-receiveDone:
		cancel()
		if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
			return nil
		}
		return err
	case <-ctx.Done():
		_ = port.Close()
		return nil
	}
}

// receiveOptions controls framing limits and ACK write deadlines for stream sources.
type receiveOptions struct {
	MaxFrameBytes   int
	ACKWriteTimeout time.Duration
}

func receive(ctx context.Context, reader io.Reader, writer io.Writer, sourceID, framingType, ackMode string, service ingest.Service) error {
	return receiveWithOptions(ctx, reader, writer, sourceID, framingType, ackMode, service, receiveOptions{})
}

func receiveWithOptions(ctx context.Context, reader io.Reader, writer io.Writer, sourceID, framingType, ackMode string, service ingest.Service, opts receiveOptions) error {
	maxFrame := opts.MaxFrameBytes
	if maxFrame <= 0 || maxFrame > lep.MaxEnvelopeSize {
		maxFrame = lep.MaxEnvelopeSize
	}
	buffer := make([]byte, 4096)
	streamDecoder := latchstream.NewDecoder(maxFrame)
	cobsDecoder := framing.NewCOBS(maxFrame)
	for {
		if ctx.Err() != nil {
			return nil
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
			var frames [][]byte
			var frameErr error
			switch framingType {
			case "cobs":
				frames, frameErr = cobsDecoder.Push(buffer[:count])
			case "latch-stream":
				frames, frameErr = streamDecoder.Push(buffer[:count])
			default:
				return fmt.Errorf("unsupported stream framing %q", framingType)
			}
			if frameErr != nil {
				ui.Warn(os.Stderr, "stream resynchronized after: "+frameErr.Error())
			}
			for _, frame := range frames {
				result, acceptErr := service.Accept(ctx, sourceID, frame)
				if ackMode != "lsak-v1" || writer == nil {
					continue
				}
				status := latchstream.AckStored
				eventID := uint32(0)
				if acceptErr != nil {
					status = ackForError(acceptErr)
				} else {
					eventID = result.Event.Envelope.EventID
					if result.Duplicate {
						status = latchstream.AckDuplicate
					}
				}
				ack := latchstream.Ack(eventID, status)
				if framingType == "cobs" {
					ack = framing.Encode(ack)
				}
				if opts.ACKWriteTimeout > 0 {
					if deadlineWriter, ok := writer.(interface{ SetWriteDeadline(time.Time) error }); ok {
						_ = deadlineWriter.SetWriteDeadline(time.Now().Add(opts.ACKWriteTimeout))
					}
				}
				if err := writeAll(writer, ack); err != nil {
					return fmt.Errorf("write ACK: %w", err)
				}
			}
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(readErr, io.EOF) {
				return io.EOF
			}
			return readErr
		}
	}
}

func ackForError(err error) latchstream.AckStatus {
	switch ingest.Code(err) {
	case ingest.CodeUnsupported:
		return latchstream.NackUnsupported
	case ingest.CodeTooLarge:
		return latchstream.NackTooLarge
	case ingest.CodeReplay:
		return latchstream.NackCorrupt
	case ingest.CodeBusy:
		return latchstream.NackBusy
	case ingest.CodeCorrupt:
		return latchstream.NackCorrupt
	default:
		return latchstream.NackInternal
	}
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		count, err := writer.Write(data)
		if err != nil {
			return err
		}
		if count <= 0 {
			return io.ErrShortWrite
		}
		data = data[count:]
	}
	return nil
}

func resultText(duplicate bool) string {
	if duplicate {
		return "duplicate"
	}
	return "stored"
}

func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	path := fs.String("config", "relay.yaml", "configuration file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}

	relay, err := store.OpenWithOptions(cfg.Instance.DataDir, store.Options{
		MaxSpoolBytes: cfg.Spool.MaxBytes, MinFreeBytes: cfg.Spool.MinFreeBytes,
		DeliveredRetention: cfg.Spool.DeliveredRetention, PressurePolicy: cfg.Spool.PressurePolicy,
		FsyncMode: cfg.Spool.Fsync, DeliveryMode: cfg.Delivery.Mode,
	})
	if err != nil {
		return err
	}
	defer relay.Close()
	if _, err := relay.ConfigureInstance(context.Background(), cfg.Instance.ID, cfg.Instance.Name, cliVersion); err != nil {
		return fmt.Errorf("configure relay identity: %w", err)
	}
	if _, err := relay.Reconcile(context.Background()); err != nil {
		return fmt.Errorf("reconcile spool: %w", err)
	}
	for _, warning := range discoverArtifacts(cfg.Artifacts.Directory, cfg.Artifacts.AutoDiscover) {
		ui.Warn(os.Stderr, "artifact discovery: "+warning.Error())
	}

	sourceTypes := map[string]string{}
	for _, source := range cfg.Sources {
		if source.IsEnabled() {
			sourceTypes[source.ID] = source.Type
		}
	}
	if err := relay.ConfigureSources(context.Background(), sourceTypes); err != nil {
		return err
	}

	storedDestinations := make([]store.Destination, 0, len(cfg.Destinations))
	runtimeDestinations := map[string]delivery.RuntimeDestination{}
	transformers := map[string]func([]byte) ([]byte, error){}
	for _, destination := range cfg.Destinations {
		if !destination.IsEnabled() {
			continue
		}
		storedDestinations = append(storedDestinations, store.Destination{
			ID: destination.ID, URL: destination.URL, AuthType: destination.Auth.Type,
			TokenRef: destination.Auth.Token, Required: destination.Required, Priority: destination.Priority,
		})
		token, err := secret.Resolve(destination.Auth.Token)
		if err != nil {
			return fmt.Errorf("destination %s auth: %w", destination.ID, err)
		}
		client, err := destinationHTTPClient(destination)
		if err != nil {
			return fmt.Errorf("destination %s TLS: %w", destination.ID, err)
		}
		runtimeDestinations[destination.ID] = delivery.RuntimeDestination{
			Token: token, Client: client, BatchEnabled: destination.Batch.Enabled,
			MaxBatchEvents: destination.Batch.MaxEvents, MaxBatchBytes: int64(destination.Batch.MaxBytes),
			PreferBinary: destination.Batch.PreferBinary, PreferZstd: destination.Batch.PreferZstd,
		}
		if policyConfig, ok := cfg.Privacy.Destinations[destination.ID]; ok {
			policy := privacy.Policy{DropTLVTypes: policyConfig.DropTLVTypes, MaxPayloadBytes: policyConfig.MaxPayloadBytes, BlockEncrypted: policyConfig.BlockEncrypted}
			if policy.Active() {
				transformers[destination.ID] = policy.Apply
			}
		}
	}
	if err := relay.ConfigureDestinations(context.Background(), storedDestinations, cfg.Delivery.Mode); err != nil {
		return err
	}

	adminToken, err := secret.Resolve(cfg.Admin.Token)
	if err != nil {
		return fmt.Errorf("admin token: %w", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	dashboard := ui.NewDashboard(ui.DashboardOptions{Title: "LAST STATE RELAY · daemon", Instance: cfg.Instance.Name, MaxLog: 300})
	publish := dashboard.Publisher()
	keyring, err := loadCryptoKeyring(cfg)
	if err != nil {
		return fmt.Errorf("crypto: %w", err)
	}
	var replay *lep.ReplayCache
	if cfg.Crypto.Enabled && cfg.Crypto.ReplayWindow > 0 {
		replay = lep.NewReplayCache(cfg.Crypto.ReplayWindow)
	}
	service := ingest.Service{
		Store: relay, Router: buildRouter(cfg),
		Keyring: keyring, Replay: replay,
		DecryptOnIngest:    cfg.Crypto.DecryptOnIngest,
		VerifyCrypto:       cfg.Crypto.Enabled && cfg.Crypto.DecryptOnIngest,
		AllowOpaqueForward: cfg.Crypto.AllowOpaqueForward || !cfg.Crypto.Enabled,
		Hooks: ingest.Hooks{
			OnAccept: func(source string, result store.Result) {
				kind := ui.EventStored
				if result.Duplicate {
					kind = ui.EventDuplicate
				}
				publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: kind, Source: source, Message: envelopeSummary(result.Event.Envelope), Detail: result.Event.ID}})
			},
			OnError: func(source string, err error) {
				publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventError, Source: source, Message: "ingest rejected", Detail: err.Error()}})
			},
		},
	}
	worker := &delivery.Worker{
		Store: relay, Destinations: runtimeDestinations, Transformers: transformers, Concurrency: cfg.Delivery.Concurrency,
		MinDelay: cfg.Delivery.Retry.MinDelay, MaxDelay: cfg.Delivery.Retry.MaxDelay,
		Multiplier: cfg.Delivery.Retry.Multiplier, Jitter: cfg.Delivery.Retry.Jitter,
		MaxAttempts:      cfg.Delivery.Retry.MaxAttempts,
		FailureThreshold: cfg.Delivery.CircuitBreaker.FailureThreshold,
		CircuitOpenFor:   cfg.Delivery.CircuitBreaker.OpenFor,
		Hooks: delivery.Hooks{
			OnDelivered: func(d store.PendingDelivery, code int) {
				publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventDelivered, Source: d.EventID, Target: d.DestinationID, Message: fmt.Sprintf("accepted HTTP %d", code)}})
			},
			OnRetry: func(d store.PendingDelivery, code int, failure string, retryIn time.Duration) {
				publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventFailed, Source: d.EventID, Target: d.DestinationID, Message: "retry scheduled", Detail: failure + " · " + retryIn.Round(time.Second).String()}})
			},
			OnDead: func(d store.PendingDelivery, code int, failure string) {
				publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventDead, Source: d.EventID, Target: d.DestinationID, Message: "dead-letter", Detail: failure}})
			},
		},
	}

	var group sync.WaitGroup
	errCh := make(chan error, len(cfg.Sources)+8)
	spawn := func(name string, fn func(context.Context) error) {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := fn(ctx); err != nil && ctx.Err() == nil {
				select {
				case errCh <- fmt.Errorf("%s: %w", name, err):
				default:
				}
				cancel()
			}
		}()
	}
	spawn("delivery", worker.Run)

	adminServer := &admin.Server{Store: relay, Ingest: service, AdminToken: adminToken, SourceID: "admin"}
	spawn("admin", func(ctx context.Context) error {
		handler := admin.RequestLoggingMiddleware(requestLogger)(adminServer.AdminHandler())
		return serveHTTP(ctx, cfg.Admin.Listen, handler, cfg.Admin.ReadTimeout, cfg.Admin.WriteTimeout, cfg.Admin.IdleTimeout, cfg.Admin.TLS)
	})
	if cfg.Metrics.Enabled {
		spawn("metrics", func(ctx context.Context) error {
			return serveHTTP(ctx, cfg.Metrics.Listen, metrics.Handler{Store: relay}, 5*time.Second, 10*time.Second, 30*time.Second, config.TLS{})
		})
	}
	for _, source := range cfg.Sources {
		if !source.IsEnabled() {
			continue
		}
		switch source.Type {
		case "directory":
			spawn("source "+source.ID, func(ctx context.Context) error { return watchDirectory(ctx, service, source, publish) })
		case "serial":
			spawn("source "+source.ID, func(ctx context.Context) error { return serialSource(ctx, service, source, publish) })
		case "tcp":
			spawn("source "+source.ID, func(ctx context.Context) error { return tcpSource(ctx, service, source, publish) })
		case "udp":
			spawn("source "+source.ID, func(ctx context.Context) error { return udpSource(ctx, service, source, publish) })
		case "http":
			token, err := secret.Resolve(source.HTTP.Token)
			if err != nil {
				return fmt.Errorf("source %s token: %w", source.ID, err)
			}
			ingestServer := &admin.Server{
				Store: relay, Ingest: service, IngestToken: token, SourceID: source.ID,
				MaxBodyBytes: source.HTTP.MaxBodyBytes, MaxConcurrent: source.HTTP.MaxConcurrent,
				SpeedLimit: admin.RateLimitConfig{
					RequestsPerSecond: source.HTTP.RequestsPerSecond,
					BurstSize:         source.HTTP.BurstSize,
					MaxKeys:           source.HTTP.RateLimitMaxKeys,
				},
			}
			spawn("source "+source.ID, func(ctx context.Context) error {
				handler := admin.RequestLoggingMiddleware(requestLogger)(ingestServer.IngestHandler())
				return serveHTTP(ctx, source.HTTP.Listen, handler, source.HTTP.ReadTimeout, source.HTTP.WriteTimeout, source.HTTP.IdleTimeout, source.HTTP.TLS)
			})
		case "mqtt":
			spawn("source "+source.ID, func(ctx context.Context) error { return mqttSource(ctx, service, source, publish) })
		case "adapter":
			spawn("source "+source.ID, func(ctx context.Context) error { return adapterSource(ctx, service, source, publish) })
		case "ble":
			spawn("source "+source.ID, func(ctx context.Context) error { return bleSource(ctx, service, source, publish) })
		case "can":
			spawn("source "+source.ID, func(ctx context.Context) error { return canSource(ctx, service, source, publish) })
		case "lorawan":
			spawn("source "+source.ID, func(ctx context.Context) error { return lorawanSource(ctx, service, source, publish) })
		}
	}
	spawn("retention", func(ctx context.Context) error {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				result, err := relay.Prune(ctx, time.Now().UTC().Add(-cfg.Spool.DeliveredRetention), false)
				if err != nil {
					publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventError, Message: "retention prune failed", Detail: err.Error()}})
					continue
				}
				if result.Events > 0 {
					publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventInfo, Message: fmt.Sprintf("pruned %d events", result.Events)}})
				}
			}
		}
	})
	spawn("status-refresh", func(ctx context.Context) error {
		start := time.Now()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			status, err := relay.Status(ctx)
			if err == nil {
				publish(ui.DashboardMsg{Snapshot: &ui.Snapshot{Events: status.Events, Pending: status.Pending, Delivered: status.Delivered, DeadLetter: status.DeadLetter, SpoolBytes: status.SpoolBytes, Uptime: time.Since(start)}})
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	})

	dashboardDone := make(chan error, 1)
	go func() { dashboardDone <- dashboard.Run(ctx); cancel() }()
	publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventInfo, Message: "relay started — press q to stop"}})

	select {
	case err := <-errCh:
		cancel()
		group.Wait()
		return err
	case err := <-dashboardDone:
		cancel()
		group.Wait()
		return err
	case <-ctx.Done():
		group.Wait()
		select {
		case err := <-errCh:
			return err
		default:
			return nil
		}
	}
}

func discoverArtifacts(artifactDir string, roots []string) []error {
	var warnings []error
	seen := map[string]bool{}
	add := func(path string) {
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		seen[path] = true
		if _, err := artifact.AddTo(artifactDir, path); err != nil {
			warnings = append(warnings, fmt.Errorf("%s: %w", path, err))
		}
	}
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("%s: %w", root, err))
			continue
		}
		if !info.IsDir() {
			add(root)
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				warnings = append(warnings, fmt.Errorf("%s: %w", path, walkErr))
				return nil
			}
			if entry.IsDir() {
				if path != root && strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			switch strings.ToLower(filepath.Ext(entry.Name())) {
			case ".elf", ".axf", ".out":
				add(path)
			}
			return nil
		})
		if err != nil {
			warnings = append(warnings, fmt.Errorf("walk %s: %w", root, err))
		}
	}
	return warnings
}

func buildRouter(cfg config.Config) ingest.Router {
	if cfg.Delivery.Mode != "route" {
		return nil
	}
	return func(sourceID string, envelope lep.Envelope) []string {
		set := map[string]bool{}
		for _, route := range cfg.Delivery.Routes {
			if route.SourceID != "" && route.SourceID != sourceID {
				continue
			}
			if len(route.EventTypes) > 0 {
				matched := false
				for _, eventType := range route.EventTypes {
					if eventType == envelope.Type {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}
			for _, target := range route.Targets {
				set[target] = true
			}
		}
		result := make([]string, 0, len(set))
		for target := range set {
			result = append(result, target)
		}
		sort.Strings(result)
		return result
	}
}

func serveHTTP(ctx context.Context, listen string, handler http.Handler, readTimeout, writeTimeout, idleTimeout time.Duration, tlsCfg config.TLS) error {
	server := &http.Server{
		Addr: listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: readTimeout, WriteTimeout: writeTimeout, IdleTimeout: idleTimeout,
		MaxHeaderBytes: 32 << 10,
	}
	if tlsCfg.Enabled {
		value, err := serverTLSConfig(tlsCfg)
		if err != nil {
			return err
		}
		server.TLSConfig = value
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		case <-done:
		}
	}()
	var err error
	if tlsCfg.Enabled {
		err = server.ListenAndServeTLS(tlsCfg.CertFile, tlsCfg.KeyFile)
	} else {
		err = server.ListenAndServe()
	}
	close(done)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func serverTLSConfig(cfg config.TLS) (*tls.Config, error) {
	value := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.RequireClientCert {
		data, err := os.ReadFile(cfg.ClientCAFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(data) {
			return nil, errors.New("client CA file contains no certificates")
		}
		value.ClientCAs = pool
		value.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return value, nil
}

func destinationHTTPClient(destination config.Destination) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 32
	transport.MaxIdleConnsPerHost = 8
	transport.IdleConnTimeout = 90 * time.Second
	transport.ResponseHeaderTimeout = 20 * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: destination.TLS.InsecureSkipVerify, ServerName: destination.TLS.ServerName} //nolint:gosec // explicit lab-only setting
	if destination.TLS.CAFile != "" {
		data, err := os.ReadFile(destination.TLS.CAFile)
		if err != nil {
			return nil, err
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(data) {
			return nil, errors.New("CA file contains no certificates")
		}
		tlsConfig.RootCAs = pool
	}
	if destination.TLS.CertFile != "" || destination.TLS.KeyFile != "" {
		certificate, err := tls.LoadX509KeyPair(destination.TLS.CertFile, destination.TLS.KeyFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

func watchDirectory(ctx context.Context, service ingest.Service, source config.Source, publish func(ui.DashboardMsg)) error {
	directory := source.DirectoryPath()
	archive := source.Directory.ArchivePath
	if archive == "" {
		archive = filepath.Join(directory, "imported")
	}
	quarantine := source.Directory.QuarantinePath
	if quarantine == "" {
		quarantine = filepath.Join(directory, "quarantine")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(archive, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(quarantine, 0700); err != nil {
		return err
	}
	ticker := time.NewTicker(source.Directory.PollInterval)
	defer ticker.Stop()
	for {
		if err := importDirectory(ctx, service, source.ID, directory, archive, quarantine, source.Directory.StableFor, source.Directory.MaxFiles, publish); err != nil && ctx.Err() == nil {
			publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventError, Source: source.ID, Message: "directory scan", Detail: err.Error()}})
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func importDirectory(ctx context.Context, service ingest.Service, sourceID, directory, archive, quarantine string, stableFor time.Duration, maxFiles int, publish func(ui.DashboardMsg)) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	processed := 0
	for _, entry := range entries {
		if processed >= maxFiles {
			break
		}
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".lep" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) < stableFor {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		raw, err := os.ReadFile(path)
		if err == nil {
			_, err = service.Accept(ctx, sourceID, raw)
		}
		if err != nil {
			if moveErr := durableMove(path, uniquePath(quarantine, entry.Name())); moveErr != nil {
				return fmt.Errorf("quarantine %s: %w", path, moveErr)
			}
			_ = os.WriteFile(filepath.Join(quarantine, entry.Name()+".error.txt"), []byte(err.Error()+"\n"), 0600)
			publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventError, Source: sourceID, Message: "quarantined " + entry.Name(), Detail: err.Error()}})
			processed++
			continue
		}
		if err := durableMove(path, uniquePath(archive, entry.Name())); err != nil {
			return err
		}
		publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventStored, Source: sourceID, Message: "imported " + entry.Name()}})
		processed++
	}
	return nil
}

func durableMove(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	if err := os.Rename(source, destination); err == nil {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".pending-move-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, input); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, destination); err != nil {
		return err
	}
	return os.Remove(source)
}

func uniquePath(directory, name string) string {
	path := filepath.Join(directory, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	return filepath.Join(directory, fmt.Sprintf("%s-%d%s", base, time.Now().UnixNano(), extension))
}

func mqttSource(ctx context.Context, service ingest.Service, src config.Source, publish func(ui.DashboardMsg)) error {
	username, err := secret.Resolve(src.MQTT.Username)
	if err != nil {
		return err
	}
	password, err := secret.Resolve(src.MQTT.Password)
	if err != nil {
		return err
	}
	var tlsConfig *tls.Config
	if src.MQTT.TLS.Enabled {
		tlsConfig, err = clientTLSConfig(src.MQTT.TLS)
		if err != nil {
			return err
		}
	}
	qos := byte(src.MQTT.QoS)
	if qos > 2 {
		qos = 1
	}
	publish(ui.DashboardMsg{Source: &ui.SourceState{ID: src.ID, Type: "mqtt", State: "up", Detail: src.MQTT.Broker}})
	return mqttsource.RunMQTT(ctx, mqttsource.MQTTConfig{
		Broker: src.MQTT.Broker, ClientID: src.MQTT.ClientID, Topics: src.MQTT.Topics,
		QoS: qos, Username: username, Password: password, TLSConfig: tlsConfig,
	}, func(topic string, payload []byte) error {
		// Treat each MQTT payload as a framing unit (raw LEP by default).
		var err error
		if src.Framing.Type == "raw" || src.Framing.Type == "" {
			_, err = service.Accept(ctx, src.ID, payload)
		} else {
			err = receiveWithOptions(ctx, bytes.NewReader(payload), io.Discard, src.ID, src.Framing.Type, "none", service, receiveOptions{
				MaxFrameBytes: src.Framing.MaxFrameBytes,
			})
		}
		if err != nil {
			// Keep the subscription up; a bad frame should not stop the source.
			publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventError, Source: src.ID, Message: "mqtt frame rejected", Detail: topic + ": " + err.Error()}})
		}
		return nil
	})
}

func adapterSource(ctx context.Context, service ingest.Service, src config.Source, publish func(ui.DashboardMsg)) error {
	proc := &adapter.Subprocess{
		Command: src.Adapter.Command,
		Args:    src.Adapter.Args,
		Env:     src.Adapter.Env,
		Dir:     src.Adapter.Dir,
		OnFrame: func(payload []byte) error {
			var err error
			if src.Framing.Type == "raw" || src.Framing.Type == "" {
				_, err = service.Accept(ctx, src.ID, payload)
			} else {
				err = receiveWithOptions(ctx, bytes.NewReader(payload), io.Discard, src.ID, src.Framing.Type, "none", service, receiveOptions{
					MaxFrameBytes: src.Framing.MaxFrameBytes,
				})
			}
			if err != nil {
				// Don't kill the adapter process over one bad frame.
				publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventError, Source: src.ID, Message: "adapter frame rejected", Detail: err.Error()}})
			}
			return nil
		},
		OnLog: func(line string) {
			publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventInfo, Source: src.ID, Message: "adapter", Detail: strings.TrimSpace(line)}})
		},
	}
	publish(ui.DashboardMsg{Source: &ui.SourceState{ID: src.ID, Type: "adapter", State: "up", Detail: src.Adapter.Command}})
	if err := proc.Start(ctx); err != nil {
		return err
	}
	defer proc.Stop()
	err := proc.Wait()
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// Experimental collectors. The BLE, CAN, and LoRaWAN transports talk to
// platform backends (a JSON-over-TCP BLE gateway, native SocketCAN on Linux,
// LoRaWAN network servers over MQTT/HTTP) and are not yet recommended for
// production. They follow the same ingest contract as the other sources:
// every collected payload is framed as raw LEP by default.
func bleSource(ctx context.Context, service ingest.Service, src config.Source, publish func(ui.DashboardMsg)) error {
	scanner := mqttsource.NewBLEScanner(mqttsource.BLEConfig{
		Adapter:         src.BLE.Adapter,
		ScanDuration:    src.BLE.ScanDuration,
		ServiceUUID:     src.BLE.ServiceUUID,
		CharUUID:        src.BLE.CharUUID,
		NotifyEnabled:   src.BLE.NotifyEnabled,
		RSSIThreshold:   src.BLE.RSSIThreshold,
		MaxDevices:      src.BLE.MaxDevices,
		FilterByAddress: src.BLE.FilterByAddress,
	}, requestLogger)
	publish(ui.DashboardMsg{Source: &ui.SourceState{ID: src.ID, Type: "ble", State: "up", Detail: src.BLE.Adapter}})
	return scanner.Run(ctx, func(_ *mqttsource.BLEDevice, _ *mqttsource.BLECharacteristic, payload []byte) error {
		_, err := service.Accept(ctx, src.ID, payload)
		if err != nil {
			publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventError, Source: src.ID, Message: "ble frame rejected", Detail: err.Error()}})
		}
		return nil
	})
}

func canSource(ctx context.Context, service ingest.Service, src config.Source, publish func(ui.DashboardMsg)) error {
	publish(ui.DashboardMsg{Source: &ui.SourceState{ID: src.ID, Type: "can", State: "up", Detail: src.CAN.Interface}})
	return mqttsource.RunCAN(ctx, mqttsource.CANConfig{
		Interface:   src.CAN.Interface,
		Baudrate:    src.CAN.Baudrate,
		FDEnabled:   src.CAN.FDEnabled,
		BRS:         src.CAN.BRS,
		FilterID:    src.CAN.FilterID,
		FilterMask:  src.CAN.FilterMask,
		Protocol:    src.CAN.Protocol,
		AdapterPath: src.CAN.AdapterPath,
		RemoteHost:  src.CAN.RemoteHost,
		RemotePort:  src.CAN.RemotePort,
	}, func(_ mqttsource.CANFrame, payload []byte) error {
		_, err := service.Accept(ctx, src.ID, payload)
		if err != nil {
			publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventError, Source: src.ID, Message: "can frame rejected", Detail: err.Error()}})
		}
		return nil
	})
}

func lorawanSource(ctx context.Context, service ingest.Service, src config.Source, publish func(ui.DashboardMsg)) error {
	client := mqttsource.NewLoRaWANClient(mqttsource.LoRaWANConfig{
		Server:     src.LoRaWAN.Server,
		APIKey:     src.LoRaWAN.APIKey,
		NetworkID:  src.LoRaWAN.NetworkID,
		DeviceEUI:  src.LoRaWAN.DeviceEUI,
		Port:       src.LoRaWAN.Port,
		Protocol:   src.LoRaWAN.Protocol,
		TLSEnabled: src.LoRaWAN.TLSEnabled,
		MaxRetries: src.LoRaWAN.MaxRetries,
	}, requestLogger)
	publish(ui.DashboardMsg{Source: &ui.SourceState{ID: src.ID, Type: "lorawan", State: "up", Detail: src.LoRaWAN.Server}})
	return client.Run(ctx, func(_ mqttsource.LoRaWANUplink, payload []byte) error {
		_, err := service.Accept(ctx, src.ID, payload)
		if err != nil {
			publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventError, Source: src.ID, Message: "lorawan frame rejected", Detail: err.Error()}})
		}
		// Returning the error lets acknowledgement-based transports (gRPC)
		// signal a NACK to the sender; the MQTT/HTTP pollers only log it.
		return err
	})
}

func clientTLSConfig(cfg config.TLS) (*tls.Config, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.InsecureSkipVerify, ServerName: cfg.ServerName} //nolint:gosec // lab-only setting controlled by operator config
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("failed to parse CA file")
		}
		tlsConfig.RootCAs = pool
	}
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return tlsConfig, nil
}

func serialSource(ctx context.Context, service ingest.Service, source config.Source, publish func(ui.DashboardMsg)) error {
	delay := source.Serial.ReconnectMin
	for {
		port, err := serial.Open(source.Serial.Device, &serial.Mode{BaudRate: source.Serial.Baud})
		if err == nil {
			_ = port.SetReadTimeout(source.Serial.ReadTimeout)
			publish(ui.DashboardMsg{Source: &ui.SourceState{ID: source.ID, Type: "serial", State: "up", Detail: source.Serial.Device}})
			err = receiveWithOptions(ctx, port, port, source.ID, source.Framing.Type, source.ACK.Mode, service, receiveOptions{
				MaxFrameBytes: source.Framing.MaxFrameBytes, ACKWriteTimeout: source.ACK.WriteTimeout,
			})
			_ = port.Close()
		}
		if ctx.Err() != nil {
			return nil
		}
		if !source.Serial.Reconnect {
			return err
		}
		publish(ui.DashboardMsg{Source: &ui.SourceState{ID: source.ID, Type: "serial", State: "reconnecting", Detail: errString(err)}})
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
		delay *= 2
		if delay > source.Serial.ReconnectMax {
			delay = source.Serial.ReconnectMax
		}
	}
}

func tcpSource(ctx context.Context, service ingest.Service, source config.Source, publish func(ui.DashboardMsg)) error {
	var listener net.Listener
	var err error
	if source.TCP.TLS.Enabled {
		tlsConfig, tlsErr := serverTLSConfig(source.TCP.TLS)
		if tlsErr != nil {
			return tlsErr
		}
		certificate, tlsErr := tls.LoadX509KeyPair(source.TCP.TLS.CertFile, source.TCP.TLS.KeyFile)
		if tlsErr != nil {
			return tlsErr
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
		listener, err = tls.Listen("tcp", source.TCP.Listen, tlsConfig)
	} else {
		listener, err = net.Listen("tcp", source.TCP.Listen)
	}
	if err != nil {
		return err
	}
	defer listener.Close()
	go func() { <-ctx.Done(); _ = listener.Close() }()
	semaphore := make(chan struct{}, source.TCP.MaxClients)
	var clients sync.WaitGroup
	defer clients.Wait()
	publish(ui.DashboardMsg{Source: &ui.SourceState{ID: source.ID, Type: "tcp", State: "up", Detail: source.TCP.Listen}})
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			connection.Close()
			return nil
		default:
			connection.Close()
			continue
		}
		clients.Add(1)
		go func(connection net.Conn) {
			defer clients.Done()
			defer func() { <-semaphore }()
			defer connection.Close()
			_ = connection.SetDeadline(time.Now().Add(source.TCP.ReadTimeout))
			_ = receiveWithOptions(ctx, connection, connection, source.ID, source.Framing.Type, source.ACK.Mode, service, receiveOptions{
				MaxFrameBytes: source.Framing.MaxFrameBytes, ACKWriteTimeout: source.ACK.WriteTimeout,
			})
		}(connection)
	}
}

func udpSource(ctx context.Context, service ingest.Service, source config.Source, publish func(ui.DashboardMsg)) error {
	address, err := net.ResolveUDPAddr("udp", source.UDP.Listen)
	if err != nil {
		return err
	}
	connection, err := net.ListenUDP("udp", address)
	if err != nil {
		return err
	}
	defer connection.Close()
	go func() { <-ctx.Done(); _ = connection.Close() }()
	allowed := make([]*net.IPNet, 0, len(source.UDP.AllowedCIDRs))
	for _, value := range source.UDP.AllowedCIDRs {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return err
		}
		allowed = append(allowed, network)
	}
	publish(ui.DashboardMsg{Source: &ui.SourceState{ID: source.ID, Type: "udp", State: "up", Detail: source.UDP.Listen}})
	buffer := make([]byte, source.UDP.MaxDatagram)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		count, remote, err := connection.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				continue
			}
			return err
		}
		if !ipAllowed(remote.IP, allowed) {
			continue
		}
		payload := append([]byte(nil), buffer[:count]...)
		frames := [][]byte{payload}
		switch source.Framing.Type {
		case "cobs":
			frames, err = framing.NewCOBS(source.Framing.MaxFrameBytes).Push(payload)
		case "latch-stream":
			frames, err = latchstream.NewDecoder(source.Framing.MaxFrameBytes).Push(payload)
		}
		if err != nil {
			publish(ui.DashboardMsg{Event: &ui.Event{Time: time.Now(), Kind: ui.EventError, Source: source.ID, Message: "invalid UDP frame", Detail: err.Error()}})
			continue
		}
		for _, frame := range frames {
			result, acceptErr := service.Accept(ctx, source.ID, frame)
			if source.ACK.Mode != "lsak-v1" {
				continue
			}
			status := latchstream.AckStored
			eventID := uint32(0)
			if acceptErr != nil {
				status = ackForError(acceptErr)
			} else {
				eventID = result.Event.Envelope.EventID
				if result.Duplicate {
					status = latchstream.AckDuplicate
				}
			}
			ack := latchstream.Ack(eventID, status)
			if source.Framing.Type == "cobs" {
				ack = framing.Encode(ack)
			}
			if _, err := connection.WriteToUDP(ack, remote); err != nil {
				return err
			}
		}
	}
}

func ipAllowed(ip net.IP, networks []*net.IPNet) bool {
	if len(networks) == 0 {
		return true
	}
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func statusCmd(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "data", "relay data directory")
	jsonOut := fs.Bool("json", false, "emit JSON only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	relay, err := dataStore(*dataDir)
	if err != nil {
		return err
	}
	defer relay.Close()
	status, err := relay.Status(context.Background())
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(status)
	}
	fmt.Fprintln(os.Stdout, ui.Section("Relay status"))
	fmt.Fprintln(os.Stdout, ui.KVTable([][2]string{
		{"relay id", status.RelayID}, {"events", fmt.Sprintf("%d", status.Events)},
		{"pending", fmt.Sprintf("%d", status.Pending)}, {"active", fmt.Sprintf("%d", status.Delivering)},
		{"delivered", fmt.Sprintf("%d", status.Delivered)}, {"dead letter", fmt.Sprintf("%d", status.DeadLetter)},
		{"spool", ui.MustFormatBytes(status.SpoolBytes)}, {"disk free", ui.MustFormatBytes(status.FreeBytes)},
		{"oldest pending", status.OldestPending.Round(time.Second).String()},
	}))
	return nil
}

func exportCmd(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "data", "relay data directory")
	output := fs.String("output", "", "output .lep file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || *output == "" {
		return errors.New("usage: export --output event.lep EVENT_ID")
	}
	relay, err := dataStore(*dataDir)
	if err != nil {
		return err
	}
	defer relay.Close()
	raw, err := relay.RawEvent(context.Background(), fs.Arg(0))
	if err != nil {
		return err
	}
	return writeFileDurable(*output, raw)
}

func writeFileDurable(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil && filepath.Dir(path) != "." {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".pending-export-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func replayCmd(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "data", "relay data directory")
	destination := fs.String("destination", "", "destination ID")
	eventID := fs.String("event", "", "event ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	relay, err := dataStore(*dataDir)
	if err != nil {
		return err
	}
	defer relay.Close()
	if err := relay.Replay(context.Background(), *eventID, *destination); err != nil {
		return err
	}
	ui.Success(os.Stdout, "delivery requeued")
	return nil
}

func doctorCmd(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "data", "relay data directory")
	jsonOut := fs.Bool("json", false, "emit JSON only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	relay, err := dataStore(*dataDir)
	if err != nil {
		return err
	}
	defer relay.Close()
	status, err := relay.Status(context.Background())
	if err != nil {
		return err
	}
	reconcile, err := relay.Reconcile(context.Background())
	if err != nil {
		return err
	}
	result := map[string]any{"status": status, "reconcile": reconcile, "healthy": status.DeadLetter == 0 && len(reconcile.MissingObjects) == 0 && len(reconcile.CorruptObjects) == 0}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	fmt.Fprintln(os.Stdout, ui.Section("Doctor"))
	fmt.Fprintln(os.Stdout, ui.KVTable([][2]string{
		{"health", fmt.Sprint(result["healthy"])}, {"data directory", relay.DataDir()},
		{"missing objects", fmt.Sprintf("%d", len(reconcile.MissingObjects))},
		{"corrupt objects", fmt.Sprintf("%d", len(reconcile.CorruptObjects))},
		{"orphan objects", fmt.Sprintf("%d", len(reconcile.OrphanObjects))},
		{"dead letter", fmt.Sprintf("%d", status.DeadLetter)}, {"disk free", ui.MustFormatBytes(status.FreeBytes)},
	}))
	if result["healthy"].(bool) {
		ui.Success(os.Stdout, "all core checks passed")
	} else {
		ui.Warn(os.Stdout, "operator attention required")
	}
	return nil
}

func artifactsCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: artifacts <add|list|inspect|verify>")
	}
	fs := flag.NewFlagSet("artifacts "+args[0], flag.ContinueOnError)
	dataDir := fs.String("data-dir", "data", "relay data directory")
	jsonOut := fs.Bool("json", false, "emit JSON only")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if fs.NArg() != 1 {
			return errors.New("usage: artifacts add firmware.elf")
		}
		item, err := artifact.Add(*dataDir, fs.Arg(0))
		if err != nil {
			return err
		}
		if *jsonOut {
			return json.NewEncoder(os.Stdout).Encode(item)
		}
		ui.Success(os.Stdout, item.SHA256+" · "+item.BuildID)
		return nil
	case "list":
		items, err := artifact.List(*dataDir)
		if err != nil {
			return err
		}
		if *jsonOut {
			return json.NewEncoder(os.Stdout).Encode(items)
		}
		table := ui.NewTable(ui.Column{Title: "sha256", Width: 18}, ui.Column{Title: "build id", Width: 22}, ui.Column{Title: "arch", Width: 14}, ui.Column{Title: "size", Width: 12}, ui.Column{Title: "name"})
		for _, item := range items {
			table.AppendRow(short(item.SHA256, 16), short(item.BuildID, 20), item.Architecture, ui.MustFormatBytes(item.Size), item.OriginalName)
		}
		fmt.Fprintln(os.Stdout, table.String())
		return nil
	case "inspect", "verify":
		if fs.NArg() != 1 {
			return errors.New("usage: artifacts " + args[0] + " PATH")
		}
		item, err := artifact.Inspect(fs.Arg(0))
		if err != nil {
			return err
		}
		if args[0] == "verify" {
			if err := artifact.Verify(item); err != nil {
				return err
			}
			ui.Success(os.Stdout, "artifact verified")
			return nil
		}
		if *jsonOut {
			return json.NewEncoder(os.Stdout).Encode(item)
		}
		fmt.Fprintln(os.Stdout, ui.KVTable([][2]string{{"sha256", item.SHA256}, {"build id", item.BuildID}, {"kind", item.Kind}, {"architecture", item.Architecture}, {"DWARF", fmt.Sprint(item.HasDWARF)}, {"size", ui.MustFormatBytes(item.Size)}, {"path", item.Path}}))
		return nil
	default:
		return errors.New("usage: artifacts <add|list|inspect|verify>")
	}
}

func configCmd(args []string) error {
	if len(args) != 2 || (args[0] != "validate" && args[0] != "show") {
		return errors.New("usage: config <validate|show> relay.yaml")
	}
	cfg, err := config.Load(args[1])
	if err != nil {
		return err
	}
	if args[0] == "show" {
		return json.NewEncoder(os.Stdout).Encode(cfg)
	}
	ui.Success(os.Stdout, fmt.Sprintf("valid · %d sources · %d destinations · %s delivery", len(cfg.Sources), len(cfg.Destinations), cfg.Delivery.Mode))
	return nil
}

func spoolCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: spool <prune|reconcile>")
	}
	fs := flag.NewFlagSet("spool "+args[0], flag.ContinueOnError)
	dataDir := fs.String("data-dir", "data", "relay data directory")
	before := fs.Duration("older-than", 30*24*time.Hour, "prune delivered events older than this")
	jsonOut := fs.Bool("json", false, "emit JSON only")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	relay, err := dataStore(*dataDir)
	if err != nil {
		return err
	}
	defer relay.Close()
	switch args[0] {
	case "prune":
		result, err := relay.Prune(context.Background(), time.Now().UTC().Add(-*before), true)
		if err != nil {
			return err
		}
		if *jsonOut {
			return json.NewEncoder(os.Stdout).Encode(result)
		}
		ui.Success(os.Stdout, fmt.Sprintf("pruned %d events and %s", result.Events, ui.MustFormatBytes(result.Bytes)))
		return nil
	case "reconcile":
		result, err := relay.Reconcile(context.Background())
		if err != nil {
			return err
		}
		if *jsonOut {
			return json.NewEncoder(os.Stdout).Encode(result)
		}
		fmt.Fprintln(os.Stdout, ui.KVTable([][2]string{{"pending files removed", fmt.Sprint(result.RemovedPendingFiles)}, {"missing objects", fmt.Sprint(len(result.MissingObjects))}, {"corrupt objects", fmt.Sprint(len(result.CorruptObjects))}, {"orphan objects", fmt.Sprint(len(result.OrphanObjects))}}))
		return nil
	default:
		return errors.New("usage: spool <prune|reconcile>")
	}
}

func destinationsCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: destinations <list|pause|resume>")
	}
	fs := flag.NewFlagSet("destinations "+args[0], flag.ContinueOnError)
	dataDir := fs.String("data-dir", "data", "relay data directory")
	jsonOut := fs.Bool("json", false, "emit JSON only")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	relay, err := dataStore(*dataDir)
	if err != nil {
		return err
	}
	defer relay.Close()
	switch args[0] {
	case "list":
		items, err := relay.DestinationStatuses(context.Background())
		if err != nil {
			return err
		}
		if *jsonOut {
			return json.NewEncoder(os.Stdout).Encode(items)
		}
		table := ui.NewTable(ui.Column{Title: "id", Width: 18}, ui.Column{Title: "state", Width: 12}, ui.Column{Title: "pending", Width: 10}, ui.Column{Title: "delivered", Width: 10}, ui.Column{Title: "dead", Width: 8}, ui.Column{Title: "url"})
		for _, item := range items {
			table.AppendRow(item.ID, item.State, fmt.Sprint(item.Pending), fmt.Sprint(item.Delivered), fmt.Sprint(item.DeadLetter), item.URL)
		}
		fmt.Fprintln(os.Stdout, table.String())
		return nil
	case "pause", "resume":
		if fs.NArg() != 1 {
			return errors.New("usage: destinations " + args[0] + " ID")
		}
		if err := relay.PauseDestination(context.Background(), fs.Arg(0), args[0] == "pause"); err != nil {
			return err
		}
		ui.Success(os.Stdout, "destination "+args[0]+"d")
		return nil
	default:
		return errors.New("usage: destinations <list|pause|resume>")
	}
}

func bundleCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: bundle <export|import>")
	}
	fs := flag.NewFlagSet("bundle "+args[0], flag.ContinueOnError)
	dataDir := fs.String("data-dir", "data", "relay data directory")
	output := fs.String("output", "", "output .lsbundle path")
	limit := fs.Int("limit", 1000, "maximum events to export")
	sourceID := fs.String("source", "bundle-import", "source ID for imported events")
	jsonOut := fs.Bool("json", false, "emit JSON only")
	signKey := fs.String("sign-key", "", "Ed25519 private key seed (32-byte hex) or PKCS-style raw for export signing")
	keyID := fs.String("key-id", "default", "signature key id written into the manifest")
	publicKey := fs.String("public-key", "", "Ed25519 public key (32-byte hex) required when --require-signature")
	requireSig := fs.Bool("require-signature", false, "reject import when signature is missing or invalid")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	relay, err := dataStore(*dataDir)
	if err != nil {
		return err
	}
	defer relay.Close()
	switch args[0] {
	case "export":
		if *output == "" {
			return errors.New("usage: bundle export --output incidents.lsbundle [--sign-key HEX --key-id ID]")
		}
		exportOpts := bundle.ExportOptions{KeyID: *keyID}
		if *signKey != "" {
			priv, err := parseEd25519Private(*signKey)
			if err != nil {
				return err
			}
			exportOpts.PrivateKey = priv
		}
		manifest, err := bundle.ExportWithOptions(context.Background(), relay, *output, *limit, exportOpts)
		if err != nil {
			return err
		}
		if *jsonOut {
			return json.NewEncoder(os.Stdout).Encode(manifest)
		}
		msg := fmt.Sprintf("exported %d events to %s", len(manifest.Events), *output)
		if len(manifest.Signatures) > 0 {
			msg += " (signed)"
		}
		ui.Success(os.Stdout, msg)
		return nil
	case "import":
		if fs.NArg() != 1 {
			return errors.New("usage: bundle import [--require-signature --public-key HEX] incidents.lsbundle")
		}
		importOpts := bundle.ImportOptions{RequireSignature: *requireSig}
		if *publicKey != "" {
			pub, err := parseEd25519Public(*publicKey)
			if err != nil {
				return err
			}
			importOpts.PublicKey = pub
		}
		result, err := bundle.ImportWithOptions(context.Background(), ingest.Service{Store: relay}, fs.Arg(0), *sourceID, importOpts)
		if err != nil {
			return err
		}
		if *jsonOut {
			return json.NewEncoder(os.Stdout).Encode(result)
		}
		ui.Success(os.Stdout, fmt.Sprintf("imported %d events · %d duplicates", result.Imported, result.Duplicates))
		return nil
	default:
		return errors.New("usage: bundle <export|import>")
	}
}

func parseEd25519Private(value string) (ed25519.PrivateKey, error) {
	raw, err := decodeKeyBytes(value)
	if err != nil {
		return nil, err
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, fmt.Errorf("ed25519 private key must be %d-byte seed or %d-byte key, got %d", ed25519.SeedSize, ed25519.PrivateKeySize, len(raw))
	}
}

func parseEd25519Public(value string) (ed25519.PublicKey, error) {
	raw, err := decodeKeyBytes(value)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("ed25519 public key must be %d bytes, got %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

func decodeKeyBytes(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "hex:") {
		return hex.DecodeString(strings.TrimPrefix(value, "hex:"))
	}
	if strings.HasPrefix(value, "file:") {
		data, err := os.ReadFile(strings.TrimSpace(strings.TrimPrefix(value, "file:")))
		if err != nil {
			return nil, err
		}
		return decodeKeyBytes(string(data))
	}
	if raw, err := hex.DecodeString(value); err == nil {
		return raw, nil
	}
	return nil, fmt.Errorf("key must be hex or hex:… or file:…")
}

func dataStore(dataDir string) (*store.Store, error) {
	relay, err := store.Open(dataDir)
	if err != nil {
		return nil, fmt.Errorf("open data store at %s: %w", dataDir, err)
	}
	return relay, nil
}

func envelopeSummary(env lep.Envelope) string {
	return fmt.Sprintf("LEP v%d type=%d arch=%d payload=%dB", env.Version, env.Type, env.Architecture, env.PayloadLength)
}
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func short(value string, count int) string {
	if len(value) <= count {
		return value
	}
	return value[:count] + "…"
}

func backupCmd(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "data", "relay data directory")
	output := fs.String("output", "", "output archive path (.zip)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		return errors.New("usage: backup --data-dir DIR --output backup.zip")
	}
	ui.Warn(os.Stdout, "stop laststate-relay before backup so the spool is quiet")
	if err := createZipArchive(*dataDir, *output); err != nil {
		return err
	}
	ui.Success(os.Stdout, "backup written to "+*output)
	return nil
}

func restoreCmd(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "data", "destination data directory")
	input := fs.String("input", "", "backup archive path (.zip)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return errors.New("usage: restore --input backup.zip --data-dir DIR")
	}
	if err := extractZipArchive(*input, *dataDir); err != nil {
		return err
	}
	ui.Success(os.Stdout, "restore completed into "+*dataDir)
	return nil
}

func createZipArchive(root, output string) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil && filepath.Dir(output) != "." {
		return err
	}
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()
	archive := zip.NewWriter(file)
	defer archive.Close()
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		writer, err := archive.Create(rel)
		if err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, input)
		input.Close()
		return copyErr
	})
}

func extractZipArchive(input, dest string) error {
	reader, err := zip.OpenReader(input)
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	for _, file := range reader.File {
		clean := filepath.Clean(file.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe path in backup: %s", file.Name)
		}
		target := filepath.Join(dest, clean)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			rc.Close()
			return err
		}
		// Bound extraction against the declared size to defeat zip bombs.
		limit := int64(16 << 20)
		if file.UncompressedSize64 > 0 && file.UncompressedSize64 < uint64(limit) {
			limit = int64(file.UncompressedSize64) + 1
		}
		_, copyErr := io.Copy(out, io.LimitReader(rc, limit))
		out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}
