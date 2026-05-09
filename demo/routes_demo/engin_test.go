package routes_demo

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"testing"
)

func middleWare() gin.HandlerFunc {
	return func(c *gin.Context) {
		fmt.Println("hello middleware.")
	}
}
func middleWare1() gin.HandlerFunc {
	return func(c *gin.Context) {
		fmt.Println("hello middleware11.")
	}
}

func en() gin.OptionFunc {
	return func(e *gin.Engine) {
		e.Use(middleWare(), middleWare1())
	}
}

func TestEngin(t *testing.T) {
	g := gin.New(en())
	use := g.Use()
	g.GET("/index", func(c *gin.Context) {
		c.String(http.StatusOK, "hello index.")
	})
	g.GET("/home", func(c *gin.Context) {
		c.String(http.StatusOK, "hello home.")
	})
	g.Run(":9090")
}
