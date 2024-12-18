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
	"github.com/spf13/pflag"
)

func main() {
	// 定义命令行参数变量
	var prometheusServerURL string
	var username string
	var password string

	// 使用pflag定义命令行参数
	pflag.StringVar(&prometheusServerURL, "server-url", "", "Prometheus server URL")
	pflag.StringVar(&username, "username", "", "Username for authentication")
	pflag.StringVar(&password, "password", "", "Password for authentication")
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
