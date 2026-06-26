package cli

import (
	"fmt"
	"io"
	"math"

	"github.com/jedib0t/go-pretty/v6/table"
)

func renderStatusTable(out io.Writer, status statusOutput) {
	summary := table.NewWriter()
	summary.SetOutputMirror(out)
	summary.SetStyle(statusTableStyle())
	summary.AppendHeader(table.Row{"Gateway", "HA Group", "Region", "AZ", "Public IP", "Datapath", "Nodes", "Desired", "Lease", "TTL", "Cache"})
	summary.AppendRow(table.Row{
		valueOrUnknown(status.GatewayID),
		valueOrUnknown(status.HAGroupID),
		valueOrUnknown(status.Region),
		valueOrUnknown(status.AvailabilityZone),
		valueOrUnknown(status.PublicIP),
		valueOrUnknown(status.Datapath),
		status.InstanceCount,
		desiredCountValue(status.DesiredCount),
		leaseGenerationValue(status.LeaseGeneration),
		leaseTTLValue(status.LeaseExpiresIn),
		cacheValue(status),
	})
	summary.Render()
	_, _ = fmt.Fprintln(out)

	instances := table.NewWriter()
	instances.SetOutputMirror(out)
	instances.SetStyle(statusTableStyle())
	instances.AppendHeader(table.Row{"Node", "Role", "Health", "State", "Age", "Version", "Private IP", "Public IP", "RX Mbps", "TX Mbps", "Metrics", "Control"})
	for _, row := range status.Instances {
		instances.AppendRow(table.Row{
			valueOrUnknown(statusRowNodeID(row)),
			valueOrUnknown(row.Role),
			valueOrUnknown(row.Health),
			valueOrUnknown(row.LifecycleState),
			ageValue(row.AgeSeconds, row.Fresh),
			valueOrUnknown(row.Version),
			valueOrUnknown(row.PrivateIP),
			valueOrUnknown(row.PublicIP),
			formatMbps(row.RXMbps),
			formatMbps(row.TXMbps),
			valueOrUnknown(row.Metrics),
			controlValue(row.ControlURL),
		})
	}
	instances.Render()
	if len(status.Warnings) > 0 {
		_, _ = fmt.Fprintln(out, "\nWarnings:")
		for _, warning := range status.Warnings {
			_, _ = fmt.Fprintf(out, "- %s\n", warning)
		}
	}
}

func statusTableStyle() table.Style {
	style := table.StyleDefault
	style.Name = "BetterNATStatus"
	style.Options = table.OptionsNoBordersAndSeparators
	return style
}

func statusRowNodeID(row statusInstanceRow) string {
	if row.NodeID != "" {
		return row.NodeID
	}
	return row.InstanceID
}

func desiredCountValue(value int32) string {
	if value == 0 {
		return "unknown"
	}
	return fmt.Sprintf("%d", value)
}

func leaseGenerationValue(value uint64) string {
	if value == 0 {
		return "unknown"
	}
	return fmt.Sprintf("%d", value)
}

func leaseTTLValue(value float64) string {
	if value == 0 {
		return "unknown"
	}
	return fmt.Sprintf("%.0fs", value)
}

func cacheValue(status statusOutput) string {
	if status.CacheMode == "" {
		return "unknown"
	}
	fresh := ""
	if status.CacheFresh != nil && !*status.CacheFresh {
		fresh = "/stale"
	}
	if status.CacheAgeSeconds > 0 {
		return fmt.Sprintf("%s%s %.1fs", status.CacheMode, fresh, status.CacheAgeSeconds)
	}
	return status.CacheMode + fresh
}

func ageValue(seconds float64, fresh bool) string {
	if seconds == 0 && !fresh {
		return "unknown"
	}
	suffix := ""
	if !fresh {
		suffix = " stale"
	}
	return fmt.Sprintf("%.1fs%s", seconds, suffix)
}

func controlValue(url string) string {
	if url == "" {
		return "unknown"
	}
	return "ok"
}

func formatMbps(value float64) string {
	if value == 0 {
		return "0.00"
	}
	if math.Abs(value) < 0.01 {
		return "<0.01"
	}
	return fmt.Sprintf("%.2f", value)
}
