package routes

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func UserHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		c.String(http.StatusOK, "Hello %s", name)
	}
}

func ProductHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Query("name")
		pwd := c.Query("pwd")
		c.String(http.StatusOK, "name = %s, pwd = %s", name, pwd)
	}
}
