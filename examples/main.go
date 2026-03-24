package main

import "github.com/gin-gonic/gin"

func handHome() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.String()
	}
}

func main() {
	g := gin.New()

	s := g.Group("/")
	s.POST("/ss", handHome())

	g.Run(":9090")
}
