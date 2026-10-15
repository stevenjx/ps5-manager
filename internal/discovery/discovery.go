package discovery

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Device struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Online   bool   `json:"online"`
}

type Discoverer struct {
	ports []int
}

func NewDiscoverer() *Discoverer {
	return &Discoverer{
		ports: []int{9090, 12800},
	}
}

// isIgnoredIP 过滤回环、本机接口 IP 以及 Docker/虚拟网卡网关地址
func isIgnoredIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}

	ipStr := ip.String()

	// 排除常见的 Docker 桥接网关 IP (如 172.16.x.1 ~ 172.31.x.1, 172.17.0.1 等)
	if strings.HasPrefix(ipStr, "172.") && strings.HasSuffix(ipStr, ".1") {
		return true
	}
	if strings.HasPrefix(ipStr, "10.") && strings.HasSuffix(ipStr, ".1") {
		return true
	}

	// 排除本机已有网卡所绑定的 IP
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok {
					if ipNet.IP.Equal(ip) {
						return true
					}
				}
			}
		}
	}

	return false
}

// verifyPS5Service 对目标端口发起微型 HTTP 探测，避免网关端口假通
func verifyPS5Service(ip string, port int, timeout time.Duration) bool {
	client := &http.Client{
		Timeout: timeout,
	}

	// 探测 DirectPackageInstaller / etaHEN API 端口
	url := fmt.Sprintf("http://%s:%d/api/is_exists", ip, port)
	req, err := http.NewRequest("POST", url, strings.NewReader(`{"title_id":"CUSA00000"}`))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusBadRequest
	}

	// 兜底基础 HTTP GET 探测
	getURL := fmt.Sprintf("http://%s:%d/", ip, port)
	getReq, err := http.NewRequest("GET", getURL, nil)
	if err != nil {
		return false
	}
	getResp, err := client.Do(getReq)
	if err == nil {
		defer getResp.Body.Close()
		return getResp.StatusCode < 500
	}

	return false
}

func (d *Discoverer) ScanSubnet(ctx context.Context, cidr string) ([]Device, error) {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	var devices []Device
	var mu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, 64)

	for currIP := ip.Mask(ipnet.Mask); ipnet.Contains(currIP); incIP(currIP) {
		targetIP := make(net.IP, len(currIP))
		copy(targetIP, currIP)

		if isIgnoredIP(targetIP) {
			continue
		}

		for _, port := range d.ports {
			wg.Add(1)
			sem <- struct{}{}

			go func(checkIP string, checkPort int) {
				defer wg.Done()
				defer func() { <-sem }()

				select {
				case <-ctx.Done():
					return
				default:
				}

				// net.JoinHostPort 会正确处理 IPv6 地址（加方括号）
				addr := net.JoinHostPort(checkIP, strconv.Itoa(checkPort))
				conn, err := net.DialTimeout("tcp", addr, 350*time.Millisecond)
				if err != nil {
					return
				}
				conn.Close()

				// 必须通过 HTTP 协议握手确认是真实 PS5 服务，杜绝网关假在线
				if !verifyPS5Service(checkIP, checkPort, 600*time.Millisecond) {
					return
				}

				proto := "DPI"
				if checkPort == 12800 {
					proto = "RPI"
				}

				mu.Lock()
				devices = append(devices, Device{
					IP:       checkIP,
					Port:     checkPort,
					Protocol: proto,
					Online:   true,
				})
				mu.Unlock()
			}(targetIP.String(), port)
		}
	}

	wg.Wait()
	return devices, nil
}

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
