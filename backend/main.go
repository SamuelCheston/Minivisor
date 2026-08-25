package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

type Config struct {
	Port int    `json:"port"`
	Name string `json:"name"`
}

func setup() {
	const configFile = "config.json"
	const daemonDir = "./daemons"

	// 检查配置文件
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fmt.Println("Config file not found, creating default config.json...")
		defaultConfig := Config{
			Port: 8080,
			Name: "Minivisor Service",
		}
		data, _ := json.MarshalIndent(defaultConfig, "", "  ")
		if err := os.WriteFile(configFile, data, 0644); err != nil {
			fmt.Printf("Error creating config file: %v\n", err)
		}
	} else {
		fmt.Println("Config file found.")
	}

	// 检查并创建 daemons 目录
	if _, err := os.Stat(daemonDir); os.IsNotExist(err) {
		fmt.Println("Daemons directory not found, creating ./daemons...")
		if err := os.Mkdir(daemonDir, 0755); err != nil {
			fmt.Printf("Error creating daemons directory: %v\n", err)
		}
	} else {
		fmt.Println("Daemons directory found.")
	}
}

func main() {
	setup()
	r := gin.Default()

	// 添加 CORS 中间件
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// 基础健康检查接口
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	// API 路由组
	api := r.Group("/api")
	{
		api.GET("/hello", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"message": "Hello from Minivisor Backend!",
			})
		})
	}

	// 启动服务，默认监听 8080 端口
	r.Run(":8080")
}
