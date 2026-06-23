package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nowakeai/betternat/internal/buildinfo"
	"github.com/nowakeai/betternat/internal/config"
)

type supportOptions struct {
	configPath string
	host       string
	outputPath string
	timeout    time.Duration
}

type supportCommandResult struct {
	Stdout []byte
	Stderr []byte
}

var runSupportCommand = func(ctx context.Context, name string, args ...string) (supportCommandResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return supportCommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
}

func newSupportCommand(ctx context.Context, out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "support",
		Short: "Collect local diagnostic support data",
	}
	opts := supportOptions{configPath: defaultConfigPath, host: defaultAgentHost(), timeout: 3 * time.Second}
	bundle := &cobra.Command{
		Use:   "bundle",
		Short: "Create a redacted local support bundle",
		RunE: func(*cobra.Command, []string) error {
			path, err := runSupportBundle(ctx, opts)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "support bundle written to %s\n", path)
			return nil
		},
	}
	bundle.Flags().StringVar(&opts.configPath, "config", opts.configPath, "agent config path")
	bundle.Flags().StringVar(&opts.host, "host", opts.host, "agent daemon endpoint")
	bundle.Flags().StringVar(&opts.outputPath, "output", opts.outputPath, "support bundle output path")
	bundle.Flags().DurationVar(&opts.timeout, "timeout", opts.timeout, "per-check timeout")
	cmd.AddCommand(bundle)
	return cmd
}

func runSupportBundle(ctx context.Context, opts supportOptions) (string, error) {
	if opts.timeout <= 0 {
		opts.timeout = 3 * time.Second
	}
	outputPath := opts.outputPath
	if outputPath == "" {
		outputPath = fmt.Sprintf("betternat-support-%s.tar.gz", time.Now().UTC().Format("20060102T150405Z"))
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil && filepath.Dir(outputPath) != "." {
		return "", err
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	writer := supportArchive{tw: tarWriter}
	writer.addJSON("metadata.json", map[string]any{
		"schema_version": "v1",
		"created_at":     time.Now().UTC().Format(time.RFC3339),
		"cli_version":    buildinfo.Current("betternat"),
	})
	cfg, cfgErr := config.LoadFile(opts.configPath)
	if cfgErr != nil {
		writer.addText("config.error.txt", cfgErr.Error()+"\n")
	} else {
		cfg.Control.PeerAPI.AuthToken = "[REDACTED]"
		writer.addJSON("config.redacted.json", cfg)
		writer.addText("metrics.prom", fetchSupportURL(ctx, prometheusURL(cfg), opts.timeout))
	}

	if status, err := requestDaemonStatus(ctx, opts.host, opts.timeout); err != nil {
		writer.addText("status.error.txt", err.Error()+"\n")
	} else {
		writer.addJSON("status.json", status)
	}
	if handover, err := requestHandover(ctx, opts.host, opts.timeout, http.MethodGet, nil); err != nil {
		writer.addText("handover-current.error.txt", err.Error()+"\n")
	} else {
		writer.addJSON("handover-current.json", handover)
	}

	for _, command := range supportCommands() {
		writer.addText(command.file, runSupportShellCommand(ctx, opts.timeout, command.name, command.args...))
	}
	return outputPath, writer.err
}

type supportArchive struct {
	tw  *tar.Writer
	err error
}

func (a *supportArchive) addJSON(name string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		a.addText(strings.TrimSuffix(name, ".json")+".error.txt", err.Error()+"\n")
		return
	}
	a.addBytes(name, append(data, '\n'))
}

func (a *supportArchive) addText(name string, value string) {
	a.addBytes(name, []byte(value))
}

func (a *supportArchive) addBytes(name string, data []byte) {
	if a.err != nil {
		return
	}
	header := &tar.Header{
		Name:    name,
		Mode:    0o600,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := a.tw.WriteHeader(header); err != nil {
		a.err = err
		return
	}
	if _, err := a.tw.Write(data); err != nil {
		a.err = err
	}
}

type supportCommand struct {
	file string
	name string
	args []string
}

func supportCommands() []supportCommand {
	return []supportCommand{
		{file: "systemd/betternat-agent.status.txt", name: "systemctl", args: []string{"status", "betternat-agent", "--no-pager"}},
		{file: "systemd/betternat-agent.journal.txt", name: "journalctl", args: []string{"-u", "betternat-agent", "--no-pager", "-n", "300"}},
		{file: "datapath/loxilb-version.txt", name: "loxicmd", args: []string{"get", "lbversion", "-o", "json"}},
		{file: "datapath/loxilb-firewall.txt", name: "loxicmd", args: []string{"get", "firewall", "-o", "json"}},
		{file: "network/ip-addr.txt", name: "ip", args: []string{"addr"}},
		{file: "network/ip-route.txt", name: "ip", args: []string{"route"}},
		{file: "network/nft-ruleset.txt", name: "nft", args: []string{"list", "ruleset"}},
	}
}

func runSupportShellCommand(ctx context.Context, timeout time.Duration, name string, args ...string) string {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := runSupportCommand(cmdCtx, name, args...)
	var out strings.Builder
	_, _ = fmt.Fprintf(&out, "$ %s %s\n", name, strings.Join(args, " "))
	if len(result.Stdout) > 0 {
		out.Write(result.Stdout)
		if !bytes.HasSuffix(result.Stdout, []byte("\n")) {
			out.WriteByte('\n')
		}
	}
	if len(result.Stderr) > 0 {
		out.WriteString("\n[stderr]\n")
		out.Write(result.Stderr)
		if !bytes.HasSuffix(result.Stderr, []byte("\n")) {
			out.WriteByte('\n')
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(&out, "\n[error]\n%v\n", err)
	}
	return out.String()
}

func fetchSupportURL(ctx context.Context, url string, timeout time.Duration) string {
	if url == "" {
		return "metrics endpoint is not configured\n"
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return err.Error() + "\n"
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return err.Error() + "\n"
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err.Error() + "\n"
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, string(body))
	}
	return string(body)
}
