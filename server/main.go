package main

import (
	"log"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/oscp/cloud-selfservice-portal-backend/server/aws"
	"github.com/oscp/cloud-selfservice-portal-backend/server/common"
	"github.com/oscp/cloud-selfservice-portal-backend/server/ddc"
	"github.com/oscp/cloud-selfservice-portal-backend/server/openshift"
	"github.com/oscp/cloud-selfservice-portal-backend/server/sematext"
)

func main() {
	router := gin.New()
	router.Use(gin.Recovery())

	// Allow cors
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowAllOrigins = true
	corsConfig.AddAllowHeaders("authorization", "*")
	router.Use(cors.New(corsConfig))

	// Public routes
	authMiddleware := common.GetAuthMiddleware()
	router.POST("/login", authMiddleware.LoginHandler)
	router.GET("/config", common.ConfigHandler)

	// Protected routes
	auth := router.Group("/api/")
	auth.Use(authMiddleware.MiddlewareFunc())
	{
		// Openshift routes
		openshift.RegisterRoutes(auth)

		// DDC routes
		ddc.RegisterRoutes(auth)

		// AWS routes
		aws.RegisterRoutes(auth)

		// Sematext routes
		sematext.RegisterRoutes(auth)
	}

	sec_api_password, ok := os.LookupEnv("SEC_API_PASSWORD")
	if ok {
		log.Println("Activating secure api (basic auth)")
		sec := router.Group("/sec", gin.BasicAuth(gin.Accounts{"SEC_API": sec_api_password}))
		openshift.RegisterSecRoutes(sec)
	} else {
		log.Println("Secure api (basic auth) won't be activated, because SEC_API_PASSWORD isn't set")
	}

	log.Println("Cloud SSP is running")
	router.Run()
}
