package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/model"
	"github.com/spf13/pflag"
)

func main() {
	// 定义命令行参数变量
	var prometheusServerURL string
	var username string
	var password string
	var nodeIP string
	var startTime string
	var endTime string

	// 使用pflag定义命令行参数
	pflag.StringVar(&prometheusServerURL, "server-url", "", "Prometheus server URL")
	pflag.StringVar(&username, "username", "", "Username for authentication")
	pflag.StringVar(&password, "password", "", "Password for authentication")
	pflag.StringVar(&nodeIP, "node-ip", "", "Node IP address to query CPU usage")
	pflag.StringVar(&startTime, "start-time", "", "Start time for query (format: 2006-01-02T15:04:05Z)")
	pflag.StringVar(&endTime, "end-time", "", "End time for query (format: 2006-01-02T15:04:05Z)")
	pflag.Parse()

	// 检查必要参数是否输入完整
	if prometheusServerURL == "" || username == "" || password == "" {
		fmt.Println("请确保输入了Prometheus服务端地址、用户名和密码，使用 --help 查看帮助信息")
		pflag.Usage()
		return
	}

	var basicAuthRoundTripper http.RoundTripper = &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			// 设置基本认证头
			req.SetBasicAuth(username, password)
			return nil, nil
		},
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 30 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // 忽略证书验证
		},
	}

	client, err := api.NewClient(api.Config{
		Address:      prometheusServerURL,
		RoundTripper: basicAuthRoundTripper,
	})

	if err != nil {
		fmt.Println("创建客户端失败:", err)
		return
	}

	// 通过客户端获取Prometheus API的v1版本实例
	v1api := v1.NewAPI(client)

	// 如果提供了节点IP和时间范围，则查询该节点的CPU利用率
	if nodeIP != "" && (startTime != "" || endTime != "") {
		queryNodeCPUUsage(v1api, nodeIP, startTime, endTime)
	} else {
		// 示例：查询某个指标（这里以查询名为'go_goroutines'的指标为例，你可按需替换）
		ctx := context.Background()
		result, warnings, err := v1api.Query(ctx, "go_goroutines", time.Now())
		if err != nil {
			fmt.Println("查询指标失败:", err)
			return
		}
		if len(warnings) > 0 {
			fmt.Println("查询有警告:", warnings)
		}

		// 处理查询结果，这里简单打印（实际根据指标类型等进行相应解析处理）
		fmt.Println("查询结果:", result)
	}

	// 以下是额外的代码部分，用于在本地启动一个HTTP服务来暴露一些客户端相关的指标（可选，看具体需求）
	// 定义一个计数器指标，用于统计访问Prometheus服务端的次数（示例）
	requestCounter := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "prometheus_server_access_total",
			Help: "Total number of accesses to the Prometheus server",
		},
	)
	prometheus.MustRegister(requestCounter)
	requestCounter.Inc()

	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(":8081", nil)
		if err != nil {
			panic(err)
		}
	}()

	// 保持程序运行，防止退出（如果有需要持续监听等情况，可按需调整）
	select {}
}

// 查询指定节点在指定时间范围内的CPU利用率
func queryNodeCPUUsage(api v1.API, nodeIP, startTimeStr, endTimeStr string) {
	ctx := context.Background()

	// 解析起止时间
	start, end, err := parseTimeRange(startTimeStr, endTimeStr)
	if err != nil {
		fmt.Println(err)
		return
	}

	step := time.Minute // 每分钟一个数据点

	// 构建查询表达式，根据节点IP过滤
	// 使用node_cpu_seconds_total指标计算CPU利用率
	// 1 - avg(rate(node_cpu_seconds_total{mode="idle",instance=~"nodeIP:.*"}[5m])) by (instance)
	query := fmt.Sprintf("1 - avg(rate(node_cpu_seconds_total{mode=\"idle\",instance=~\"%s:.*\"}[5m])) by (instance)", nodeIP)

	// 执行范围查询
	result, warnings, err := api.QueryRange(ctx, query, v1.Range{
		Start: start,
		End:   end,
		Step:  step,
	})

	if err != nil {
		fmt.Printf("查询节点 %s 的CPU利用率失败: %v\n", nodeIP, err)
		return
	}

	if len(warnings) > 0 {
		fmt.Println("查询有警告:", warnings)
	}

	// 处理并打印结果
	fmt.Printf("节点 %s 在 %s 至 %s 的CPU利用率:\n",
		nodeIP,
		start.Format("2006-01-02 15:04:05"),
		end.Format("2006-01-02 15:04:05"))

	// 检查结果类型并相应处理
	matrix, ok := result.(model.Matrix)
	if !ok {
		fmt.Println("查询结果类型不是时间序列矩阵")
		fmt.Println("原始结果:", result)
		return
	}

	if len(matrix) == 0 {
		fmt.Printf("未找到节点 %s 的CPU利用率数据\n", nodeIP)
		return
	}

	// 遍历每个时间序列
	for _, series := range matrix {
		fmt.Printf("实例: %s\n", series.Metric["instance"])
		for _, sample := range series.Values {
			timestamp := time.Unix(int64(sample.Timestamp)/1000, 0)
			cpuUsage := float64(sample.Value) * 100 // 转换为百分比
			fmt.Printf("  %s: %.2f%%\n", timestamp.Format("2006-01-02 15:04:05"), cpuUsage)
		}
	}
}

// 解析时间范围字符串为时间对象
func parseTimeRange(startTimeStr, endTimeStr string) (time.Time, time.Time, error) {
	// 时间格式：2006-01-02T15:04:05Z
	timeFormat := "2006-01-02T15:04:05Z"

	// 如果未提供结束时间，使用当前时间
	end := time.Now()
	if endTimeStr != "" {
		parsedEnd, err := time.Parse(timeFormat, endTimeStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("无法解析结束时间 %s: %v", endTimeStr, err)
		}
		end = parsedEnd
	}

	// 如果未提供开始时间，默认使用结束时间前1小时
	start := end.Add(-1 * time.Hour)
	if startTimeStr != "" {
		parsedStart, err := time.Parse(timeFormat, startTimeStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("无法解析开始时间 %s: %v", startTimeStr, err)
		}
		start = parsedStart
	}

	// 确保开始时间早于结束时间
	if start.After(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("开始时间 %s 晚于结束时间 %s",
			start.Format(timeFormat), end.Format(timeFormat))
	}

	return start, end, nil
}
