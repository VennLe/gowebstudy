package main

import (
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
	"log"
	"net/http"
	"time"
)

var eg errgroup.Group

func handHome() gin.HandlerFunc {
	return func(c *gin.Context) {
	}
}

func server01() http.Handler {
	s1 := gin.New()
	s1.GET("/home", handHome())
	return s1
}

func server02() http.Handler {
	s2 := gin.New()
	s2.GET("/index", handHome())
	return s2
}

func main() {
	server1 := http.Server{
		Addr:         ":9090",
		Handler:      server01(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	server2 := http.Server{
		Addr:         ":8080",
		Handler:      server02(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	eg.Go(func() error {
		return server1.ListenAndServe()
	})
	eg.Go(func() error {
		return server2.ListenAndServe()
	})
	if err := eg.Wait(); err != nil {
		log.Println("程序退出！")
	}
	log.Println("服务器启动成功！")
}
