package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func parsePorts(spec string) ([]int, error) {
	// Accepts formats like: "80,443,8080,21-25"
	set := make(map[int]struct{})
	parts := strings.Split(spec, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(p, "-") {
			bounds := strings.SplitN(p, "-", 2)
			if len(bounds) != 2 {
				return nil, fmt.Errorf("invalid range: %s", p)
			}
			start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid port: %s", bounds[0])
			}
			end, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid port: %s", bounds[1])
			}
			if start < 1 || end < 1 || start > 65535 || end > 65535 || start > end {
				return nil, fmt.Errorf("invalid range bounds: %s", p)
			}
			for i := start; i <= end; i++ {
				set[i] = struct{}{}
			}
		} else {
			n, err := strconv.Atoi(p)
			if err != nil {
				return nil, fmt.Errorf("invalid port: %s", p)
			}
			if n < 1 || n > 65535 {
				return nil, fmt.Errorf("port out of range: %d", n)
			}
			set[n] = struct{}{}
		}
	}
	ports := make([]int, 0, len(set))
	for k := range set {
		ports = append(ports, k)
	}
	sort.Ints(ports)
	return ports, nil
}

func worker(host string, ports <-chan int, results chan<- int, timeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	for p := range ports {
		addr := fmt.Sprintf("%s:%d", host, p)
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err == nil {
			_ = conn.Close()
			results <- p // send only open ports
		}
	}
}

func main() {
	var (
		hostFlag    = flag.String("host", "", "Target host (name or IP), required")
		portsFlag   = flag.String("ports", "1-1024", "Ports to scan (e.g. 80,443,8080,21-25 or 1-65535)")
		workersFlag = flag.Int("workers", 100, "Number of concurrent workers (goroutines)")
		timeoutFlag = flag.Int("timeout", 500, "Dial timeout in milliseconds")
	)

	// Custom help output
	flag.usage = func() {}
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `
pscanner - Fast TCP port scanner

Usage:
  pscanner --host <host> [--ports 1-1024] [--workers 100] [--timeout 500]

Options:
  --host     Target host (domain name or IP) [required]
  --ports    Ports to scan, supports single ports and ranges (default: 1-1024)
             Example: "80,443,8080,21-25"
  --workers  Number of concurrent workers (default: 100)
  --timeout  Dial timeout in milliseconds (default: 500)
  --help     Show this help message

Example:
  pscanner --host example.com --ports 80,443,8000-8100 --workers 200 --timeout 300
`)
	}

	flag.Parse()

	if *hostFlag == "" {
		fmt.Fprintln(os.Stderr, "error: --host is required")
		flag.Usage()
		os.Exit(2)
	}

	if *workersFlag <= 0 {
		fmt.Fprintln(os.Stderr, "error: --workers must be > 0")
		os.Exit(2)
	}
	if *workersFlag > 10000 {
		fmt.Fprintln(os.Stderr, "error: --workers too large (max 10000)")
		os.Exit(2)
	}

	ports, err := parsePorts(*portsFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing ports: %v\n", err)
		os.Exit(2)
	}
	if len(ports) == 0 {
		fmt.Fprintln(os.Stderr, "no ports to scan")
		os.Exit(0)
	}

	portsCh := make(chan int, *workersFlag)
	resultsCh := make(chan int)
	var wg sync.WaitGroup
	timeout := time.Duration(*timeoutFlag) * time.Millisecond

	numWorkers := *workersFlag
	if numWorkers > len(ports) {
		numWorkers = len(ports)
	}
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(*hostFlag, portsCh, resultsCh, timeout, &wg)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	go func() {
		for _, p := range ports {
			portsCh <- p
		}
		close(portsCh)
	}()

	var open []int
	for p := range resultsCh {
		open = append(open, p)
	}

	sort.Ints(open)
	fmt.Printf("Host: %s\n", *hostFlag)
	fmt.Printf("Scanned ports: %d\n", len(ports))
	fmt.Printf("Workers used: %d\n", numWorkers)
	fmt.Printf("Timeout: %dms\n", *timeoutFlag)
	fmt.Println("Open ports:")
	if len(open) == 0 {
		fmt.Println("  (none found)")
	} else {
		for _, p := range open {
			fmt.Printf("  %d\n", p)
		}
	}
}
