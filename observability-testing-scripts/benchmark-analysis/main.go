package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// BenchmarkResults represents the structure of exported metrics JSON
type BenchmarkResults struct {
	Metadata struct {
		Scenario        string `json:"scenario"`
		StartTime       int64  `json:"start_time"`
		EndTime         int64  `json:"end_time"`
		DurationSeconds int    `json:"duration_seconds"`
		Timestamp       string `json:"timestamp"`
	} `json:"metadata"`

	SystemMetrics struct {
		ServiceCPU struct {
			Data PrometheusResponse `json:"data"`
		} `json:"service_cpu"`
		ServiceMemory struct {
			Data PrometheusResponse `json:"data"`
		} `json:"service_memory"`
		OtelCollectorCPU struct {
			Data PrometheusResponse `json:"data"`
		} `json:"otel_collector_cpu"`
		OtelCollectorMemory struct {
			Data PrometheusResponse `json:"data"`
		} `json:"otel_collector_memory"`
		ClusterTotalCPU struct {
			Data PrometheusResponse `json:"data"`
		} `json:"cluster_total_cpu"`
		ClusterTotalMemory struct {
			Data PrometheusResponse `json:"data"`
		} `json:"cluster_total_memory"`
	} `json:"system_metrics"`

	ValidationMetrics struct {
		AuctionRuns struct {
			Data PrometheusResponse `json:"data"`
		} `json:"auction_runs"`
		FlowThroughput struct {
			Data PrometheusResponse `json:"data"`
		} `json:"flow_throughput"`
		ClearingPrice struct {
			Data PrometheusResponse `json:"data"`
		} `json:"clearing_price"`
	} `json:"validation_metrics"`
}

// PrometheusResponse represents Prometheus query_range API response
type PrometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string        `json:"resultType"`
		Result     []MetricValue `json:"result"`
	} `json:"data"`
}

// MetricValue represents a single metric time series
type MetricValue struct {
	Metric map[string]string `json:"metric"`
	Values [][]interface{}   `json:"values"`
}

// ServiceMetrics aggregates CPU and memory for a service
type ServiceMetrics struct {
	Name      string
	AvgCPU    float64
	AvgMemory float64
	DataPoints int
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: benchmark-analysis <scenario-a.json> <scenario-b.json>\n")
		os.Exit(1)
	}

	scenarioA := loadResults(os.Args[1])
	scenarioB := loadResults(os.Args[2])

	fmt.Println("==============================================")
	fmt.Println("Telemetry Architecture Benchmark Comparison")
	fmt.Println("==============================================")
	fmt.Println()

	// Validate both scenarios ran for same duration
	if scenarioA.Metadata.DurationSeconds != scenarioB.Metadata.DurationSeconds {
		fmt.Printf("⚠️  WARNING: Scenarios have different durations (A: %ds, B: %ds)\n\n",
			scenarioA.Metadata.DurationSeconds, scenarioB.Metadata.DurationSeconds)
	}

	// Validate workloads match
	validateWorkloads(scenarioA, scenarioB)

	// Compare system metrics
	compareSystemMetrics(scenarioA, scenarioB)

	// Generate markdown report
	generateReport(scenarioA, scenarioB)
}

func loadResults(path string) *BenchmarkResults {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to read %s: %v\n", path, err)
		os.Exit(1)
	}

	var results BenchmarkResults
	if err := json.Unmarshal(data, &results); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to parse JSON from %s: %v\n", path, err)
		os.Exit(1)
	}

	return &results
}

func validateWorkloads(a, b *BenchmarkResults) {
	fmt.Println("Validation: Workload Consistency")
	fmt.Println("----------------------------")

	// Compare auction runs
	auctionsA := countTotalValues(a.ValidationMetrics.AuctionRuns.Data)
	auctionsB := countTotalValues(b.ValidationMetrics.AuctionRuns.Data)

	if auctionsA > 0 && auctionsB > 0 {
		diff := ((auctionsB - auctionsA) / auctionsA) * 100
		fmt.Printf("  Auction runs: A: %.0f | B: %.0f | Diff: %+.2f%%\n", auctionsA, auctionsB, diff)

		if abs(diff) > 5 {
			fmt.Printf("    ⚠️  WARNING: Auction count differs by more than 5%% - workloads may not be comparable\n")
		} else {
			fmt.Printf("    ✅ Workloads match\n")
		}
	} else {
		fmt.Printf("  ⚠️  Cannot validate auction runs (no data)\n")
	}

	fmt.Println()
}

func compareSystemMetrics(a, b *BenchmarkResults) {
	fmt.Println("System Resource Usage Comparison")
	fmt.Println("=================================")
	fmt.Println()

	// Per-service metrics
	fmt.Println("Per-Service Metrics:")
	fmt.Println("----------------------------")

	servicesA := aggregateServiceMetrics(a)
	servicesB := aggregateServiceMetrics(b)

	// Combine all service names
	allServices := make(map[string]bool)
	for name := range servicesA {
		allServices[name] = true
	}
	for name := range servicesB {
		allServices[name] = true
	}

	totalCPUDiffSum := 0.0
	totalMemDiffSum := 0.0
	serviceCount := 0

	for serviceName := range allServices {
		metricsA, okA := servicesA[serviceName]
		metricsB, okB := servicesB[serviceName]

		if !okA || !okB {
			fmt.Printf("  %s: ⚠️  Missing data in one scenario\n", serviceName)
			continue
		}

		cpuDiff := ((metricsB.AvgCPU - metricsA.AvgCPU) / metricsA.AvgCPU) * 100
		memDiff := ((metricsB.AvgMemory - metricsA.AvgMemory) / metricsA.AvgMemory) * 100

		fmt.Printf("\n  %s:\n", serviceName)
		fmt.Printf("    CPU:    A: %.4f cores | B: %.4f cores | Diff: %+.2f%%\n",
			metricsA.AvgCPU, metricsB.AvgCPU, cpuDiff)
		fmt.Printf("    Memory: A: %.2f MB   | B: %.2f MB   | Diff: %+.2f%%\n",
			metricsA.AvgMemory/1024/1024, metricsB.AvgMemory/1024/1024, memDiff)

		totalCPUDiffSum += cpuDiff
		totalMemDiffSum += memDiff
		serviceCount++
	}

	avgCPUDiff := totalCPUDiffSum / float64(serviceCount)
	avgMemDiff := totalMemDiffSum / float64(serviceCount)

	fmt.Println()
	fmt.Printf("  Average per-service: CPU %+.2f%% | Memory %+.2f%%\n", avgCPUDiff, avgMemDiff)

	// Cluster-level metrics
	fmt.Println()
	fmt.Println("Cluster-Level Totals:")
	fmt.Println("----------------------------")

	clusterA := aggregateClusterMetrics(a)
	clusterB := aggregateClusterMetrics(b)

	totalCPUDiff := ((clusterB.AvgCPU - clusterA.AvgCPU) / clusterA.AvgCPU) * 100
	totalMemDiff := ((clusterB.AvgMemory - clusterA.AvgMemory) / clusterA.AvgMemory) * 100

	fmt.Printf("  Total CPU:    A: %.4f cores | B: %.4f cores | Diff: %+.2f%%\n",
		clusterA.AvgCPU, clusterB.AvgCPU, totalCPUDiff)
	fmt.Printf("  Total Memory: A: %.2f GB   | B: %.2f GB   | Diff: %+.2f%%\n",
		clusterA.AvgMemory/1024/1024/1024, clusterB.AvgMemory/1024/1024/1024, totalMemDiff)

	// OTel Collector overhead (only in Scenario A)
	fmt.Println()
	fmt.Println("OTel Collector Overhead (Scenario A only):")
	fmt.Println("----------------------------")

	otelMetrics := extractOtelCollectorMetrics(a)
	if otelMetrics.DataPoints > 0 {
		fmt.Printf("  CPU:    %.4f cores\n", otelMetrics.AvgCPU)
		fmt.Printf("  Memory: %.2f MB\n", otelMetrics.AvgMemory/1024/1024)

		otelCPUPct := (otelMetrics.AvgCPU / clusterA.AvgCPU) * 100
		otelMemPct := (otelMetrics.AvgMemory / clusterA.AvgMemory) * 100
		fmt.Printf("  As %% of cluster: CPU: %.2f%% | Memory: %.2f%%\n", otelCPUPct, otelMemPct)
	} else {
		fmt.Printf("  ⚠️  No OTel Collector metrics found (may not be running)\n")
	}

	// Summary
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Println("----------------------------")
	if totalCPUDiff < 0 {
		fmt.Printf("✅ Scenario B (Direct) reduced cluster CPU by %.2f%%\n", abs(totalCPUDiff))
	} else {
		fmt.Printf("⚠️  Scenario B (Direct) increased cluster CPU by %.2f%%\n", totalCPUDiff)
	}

	if totalMemDiff < 0 {
		fmt.Printf("✅ Scenario B (Direct) reduced cluster memory by %.2f%%\n", abs(totalMemDiff))
	} else {
		fmt.Printf("⚠️  Scenario B (Direct) increased cluster memory by %.2f%%\n", totalMemDiff)
	}

	fmt.Println()
	fmt.Println("Trade-offs:")
	fmt.Println("  Scenario A (Collector): Centralized config, vendor-agnostic, easier ops")
	fmt.Println("  Scenario B (Direct):    Lower overhead, tighter coupling to backends")
	fmt.Println()
}

// extractServiceName extracts service name from pod name
// e.g., "api-gateway-569d88969d-slbhj" -> "api-gateway"
func extractServiceName(podName string) string {
	// Pod names have format: service-name-replicaset-pod
	// We need to strip the last two parts (replicaset ID and pod ID)
	parts := strings.Split(podName, "-")
	if len(parts) >= 3 {
		// Find the last part that looks like a replicaset ID (alphanumeric, 9-10 chars)
		for i := len(parts) - 2; i >= 0; i-- {
			if len(parts[i]) >= 8 && len(parts[i]) <= 10 {
				// This is likely the replicaset ID, so join everything before it
				return strings.Join(parts[:i], "-")
			}
		}
	}
	// Fallback: return the pod name as-is
	return podName
}

func aggregateServiceMetrics(results *BenchmarkResults) map[string]ServiceMetrics {
	// First pass: collect all pod data grouped by service
	cpuByService := make(map[string][]float64)
	memByService := make(map[string][]float64)

	// Aggregate CPU
	for _, result := range results.SystemMetrics.ServiceCPU.Data.Data.Result {
		podName := result.Metric["pod"]
		if podName == "" {
			continue
		}

		// Extract service name from pod name (e.g., "api-gateway-abc123-xyz" -> "api-gateway")
		serviceName := extractServiceName(podName)

		// Calculate average for this pod
		var sum float64
		count := 0
		for _, value := range result.Values {
			if cpuStr, ok := value[1].(string); ok {
				if cpu, err := strconv.ParseFloat(cpuStr, 64); err == nil {
					sum += cpu
					count++
				}
			}
		}

		if count > 0 {
			cpuByService[serviceName] = append(cpuByService[serviceName], sum/float64(count))
		}
	}

	// Aggregate memory
	for _, result := range results.SystemMetrics.ServiceMemory.Data.Data.Result {
		podName := result.Metric["pod"]
		if podName == "" {
			continue
		}

		// Extract service name from pod name
		serviceName := extractServiceName(podName)

		// Calculate average for this pod
		var sum float64
		count := 0
		for _, value := range result.Values {
			if memStr, ok := value[1].(string); ok {
				if mem, err := strconv.ParseFloat(memStr, 64); err == nil {
					sum += mem
					count++
				}
			}
		}

		if count > 0 {
			memByService[serviceName] = append(memByService[serviceName], sum/float64(count))
		}
	}

	// Second pass: average across all pods of each service
	metrics := make(map[string]ServiceMetrics)
	allServices := make(map[string]bool)
	for svc := range cpuByService {
		allServices[svc] = true
	}
	for svc := range memByService {
		allServices[svc] = true
	}

	for serviceName := range allServices {
		m := ServiceMetrics{Name: serviceName}

		// Average CPU across all pods
		if cpus, ok := cpuByService[serviceName]; ok && len(cpus) > 0 {
			var sum float64
			for _, cpu := range cpus {
				sum += cpu
			}
			m.AvgCPU = sum / float64(len(cpus))
			m.DataPoints = len(cpus)
		}

		// Average memory across all pods
		if mems, ok := memByService[serviceName]; ok && len(mems) > 0 {
			var sum float64
			for _, mem := range mems {
				sum += mem
			}
			m.AvgMemory = sum / float64(len(mems))
		}

		metrics[serviceName] = m
	}

	return metrics
}

func aggregateClusterMetrics(results *BenchmarkResults) ServiceMetrics {
	var m ServiceMetrics

	// CPU
	if len(results.SystemMetrics.ClusterTotalCPU.Data.Data.Result) > 0 {
		var sum float64
		count := 0
		for _, value := range results.SystemMetrics.ClusterTotalCPU.Data.Data.Result[0].Values {
			if cpuStr, ok := value[1].(string); ok {
				if cpu, err := strconv.ParseFloat(cpuStr, 64); err == nil {
					sum += cpu
					count++
				}
			}
		}
		if count > 0 {
			m.AvgCPU = sum / float64(count)
		}
	}

	// Memory
	if len(results.SystemMetrics.ClusterTotalMemory.Data.Data.Result) > 0 {
		var sum float64
		count := 0
		for _, value := range results.SystemMetrics.ClusterTotalMemory.Data.Data.Result[0].Values {
			if memStr, ok := value[1].(string); ok {
				if mem, err := strconv.ParseFloat(memStr, 64); err == nil {
					sum += mem
					count++
				}
			}
		}
		if count > 0 {
			m.AvgMemory = sum / float64(count)
			m.DataPoints = count
		}
	}

	return m
}

func extractOtelCollectorMetrics(results *BenchmarkResults) ServiceMetrics {
	var m ServiceMetrics

	// CPU
	if len(results.SystemMetrics.OtelCollectorCPU.Data.Data.Result) > 0 {
		var sum float64
		count := 0
		for _, value := range results.SystemMetrics.OtelCollectorCPU.Data.Data.Result[0].Values {
			if cpuStr, ok := value[1].(string); ok {
				if cpu, err := strconv.ParseFloat(cpuStr, 64); err == nil {
					sum += cpu
					count++
				}
			}
		}
		if count > 0 {
			m.AvgCPU = sum / float64(count)
			m.DataPoints = count
		}
	}

	// Memory
	if len(results.SystemMetrics.OtelCollectorMemory.Data.Data.Result) > 0 {
		var sum float64
		count := 0
		for _, value := range results.SystemMetrics.OtelCollectorMemory.Data.Data.Result[0].Values {
			if memStr, ok := value[1].(string); ok {
				if mem, err := strconv.ParseFloat(memStr, 64); err == nil {
					sum += mem
					count++
				}
			}
		}
		if count > 0 {
			m.AvgMemory = sum / float64(count)
		}
	}

	return m
}

func countTotalValues(resp PrometheusResponse) float64 {
	var total float64
	for _, result := range resp.Data.Result {
		for _, value := range result.Values {
			if valStr, ok := value[1].(string); ok {
				if val, err := strconv.ParseFloat(valStr, 64); err == nil {
					total += val
				}
			}
		}
	}
	return total
}

func generateReport(a, b *BenchmarkResults) {
	report := strings.Builder{}

	report.WriteString("# Telemetry Architecture Benchmark Report\n\n")
	report.WriteString(fmt.Sprintf("**Generated:** %s\n\n", time.Now().Format("2006-01-02 15:04:05")))
	report.WriteString(fmt.Sprintf("**Duration:** %d seconds (%d minutes)\n\n",
		a.Metadata.DurationSeconds, a.Metadata.DurationSeconds/60))

	report.WriteString("## Executive Summary\n\n")

	clusterA := aggregateClusterMetrics(a)
	clusterB := aggregateClusterMetrics(b)

	cpuImprovement := ((clusterA.AvgCPU - clusterB.AvgCPU) / clusterA.AvgCPU) * 100
	memImprovement := ((clusterA.AvgMemory - clusterB.AvgMemory) / clusterA.AvgMemory) * 100

	if cpuImprovement > 0 {
		report.WriteString(fmt.Sprintf("- **Scenario B** (Direct) reduced cluster CPU by **%.2f%%**\n", cpuImprovement))
	} else {
		report.WriteString(fmt.Sprintf("- **Scenario B** (Direct) increased cluster CPU by **%.2f%%**\n", abs(cpuImprovement)))
	}

	if memImprovement > 0 {
		report.WriteString(fmt.Sprintf("- **Scenario B** (Direct) reduced cluster memory by **%.2f%%**\n", memImprovement))
	} else {
		report.WriteString(fmt.Sprintf("- **Scenario B** (Direct) increased cluster memory by **%.2f%%**\n", abs(memImprovement)))
	}

	// OTel Collector overhead
	otelMetrics := extractOtelCollectorMetrics(a)
	if otelMetrics.DataPoints > 0 {
		otelCPUPct := (otelMetrics.AvgCPU / clusterA.AvgCPU) * 100
		otelMemPct := (otelMetrics.AvgMemory / clusterA.AvgMemory) * 100
		report.WriteString(fmt.Sprintf("- **OTel Collector** consumed **%.2f%%** of cluster CPU and **%.2f%%** of memory in Scenario A\n",
			otelCPUPct, otelMemPct))
	}

	report.WriteString("\n## Detailed Results\n\n")
	report.WriteString("### Per-Service Resource Usage\n\n")
	report.WriteString("| Service | Scenario A CPU | Scenario B CPU | CPU Diff % | Scenario A Memory | Scenario B Memory | Memory Diff % |\n")
	report.WriteString("|---------|----------------|----------------|------------|-------------------|-------------------|---------------|\n")

	servicesA := aggregateServiceMetrics(a)
	servicesB := aggregateServiceMetrics(b)

	allServices := make(map[string]bool)
	for name := range servicesA {
		allServices[name] = true
	}
	for name := range servicesB {
		allServices[name] = true
	}

	for service := range allServices {
		metricsA, okA := servicesA[service]
		metricsB, okB := servicesB[service]

		if !okA || !okB {
			continue
		}

		cpuDiff := ((metricsB.AvgCPU - metricsA.AvgCPU) / metricsA.AvgCPU) * 100
		memDiff := ((metricsB.AvgMemory - metricsA.AvgMemory) / metricsA.AvgMemory) * 100

		report.WriteString(fmt.Sprintf("| %s | %.4f cores | %.4f cores | %+.2f%% | %.2f MB | %.2f MB | %+.2f%% |\n",
			service,
			metricsA.AvgCPU, metricsB.AvgCPU, cpuDiff,
			metricsA.AvgMemory/1024/1024, metricsB.AvgMemory/1024/1024, memDiff))
	}

	report.WriteString("\n### Cluster-Level Totals\n\n")
	report.WriteString("| Metric | Scenario A | Scenario B | Diff % |\n")
	report.WriteString("|--------|------------|------------|--------|\n")
	report.WriteString(fmt.Sprintf("| Total CPU | %.4f cores | %.4f cores | %+.2f%% |\n",
		clusterA.AvgCPU, clusterB.AvgCPU, ((clusterB.AvgCPU-clusterA.AvgCPU)/clusterA.AvgCPU)*100))
	report.WriteString(fmt.Sprintf("| Total Memory | %.2f GB | %.2f GB | %+.2f%% |\n",
		clusterA.AvgMemory/1024/1024/1024, clusterB.AvgMemory/1024/1024/1024,
		((clusterB.AvgMemory-clusterA.AvgMemory)/clusterA.AvgMemory)*100))

	report.WriteString("\n## Conclusion\n\n")
	report.WriteString("**Trade-offs:**\n\n")
	report.WriteString("- **Scenario A (OTel Collector):**\n")
	report.WriteString("  - Centralized configuration\n")
	report.WriteString("  - Vendor-agnostic (easy to swap backends)\n")
	report.WriteString("  - Easier operational management\n")
	report.WriteString("  - Additional resource overhead\n\n")
	report.WriteString("- **Scenario B (Direct):**\n")
	report.WriteString("  - Lower resource overhead\n")
	report.WriteString("  - Direct control over exporters\n")
	report.WriteString("  - Tighter coupling to specific backends\n")
	report.WriteString("  - More complex configuration per service\n")

	// Save report
	reportPath := "../../data/benchmark-report.md"
	if err := os.WriteFile(reportPath, []byte(report.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to write report: %v\n", err)
	} else {
		fmt.Printf("✅ Detailed report saved to: %s\n", reportPath)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
