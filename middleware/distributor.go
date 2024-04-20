package middleware

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/ctxkey"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/model"
	"github.com/songquanpeng/one-api/relay/channeltype"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type ModelRequest struct {
	Model string `json:"model"`
}

func Distribute() func(c *gin.Context) {
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		userGroup, _ := model.CacheGetUserGroup(userId)
		c.Set("group", userGroup)
		var requestModel string
		var channel *model.Channel
		channelId, ok := c.Get("specific_channel_id")
		if ok {
			id, err := strconv.Atoi(channelId.(string))
			if err != nil {
				abortWithMessage(c, http.StatusBadRequest, "Invalid server node ID")
				return
			}
			channel, err = model.GetChannelById(id, true)
			if err != nil {
				abortWithMessage(c, http.StatusBadRequest, "Invalid server node ID")
				return
			}
			if channel.Status != model.ChannelStatusEnabled {
				abortWithMessage(c, http.StatusForbidden, "This service node has been disabled")
				return
			}
		} else {
			requestModel = c.GetString("request_model")
			var err error
			// Support GPTs
			re := regexp.MustCompile(`gpt-4-gizmo-g-[a-zA-Z0-9]{9}`)
			if requestModel == "gpt-4-gizmo-*" || requestModel == "gpt-4-gizmo-g" || (strings.HasPrefix(requestModel, "gpt-4-gizmo-g") && !re.MatchString(requestModel)) {
				message := "Please specify a specific model, GPTs share links typified by strings such as 'g-xxxxxxxxx', featuring an 11-character string that include 'g-'. The complete model name appears as follows: 'gpt-4-gizmo-g-xxxxxxxxx'."
				abortWithMessage(c, http.StatusServiceUnavailable, message)
				return
			} else if strings.HasPrefix(requestModel, "gpt-4-gizmo-g") {
				requestModel = "gpt-4-gizmo-*"
			}
			channel, err = model.CacheGetRandomSatisfiedChannel(userGroup, requestModel, false)
			if err != nil {
				message := fmt.Sprintf("No available service nodes for model %s", requestModel)
				if channel != nil {
					logger.SysError(fmt.Sprintf("Service node does not exist: %d", channel.Id))
					message = "数据库一致性已被破坏，请联系管理员"
				}
				abortWithMessage(c, http.StatusServiceUnavailable, message)
				return
			}
		}
		SetupContextForSelectedChannel(c, channel, requestModel)
		c.Next()
	}
}

func SetupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string) {
	c.Set("channel", channel.Type)
	c.Set("channel_id", channel.Id)
	c.Set("channel_name", channel.Name)
	c.Set("model_mapping", channel.GetModelMapping())
	c.Set(ctxkey.OriginalModel, modelName) // for retry
	c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", channel.Key))
	c.Set("base_url", channel.GetBaseURL())
	// this is for backward compatibility
	switch channel.Type {
	case channeltype.Azure:
		c.Set(config.KeyAPIVersion, channel.Other)
	case channeltype.Xunfei:
		c.Set(config.KeyAPIVersion, channel.Other)
	case channeltype.Gemini:
		c.Set(config.KeyAPIVersion, channel.Other)
	case channeltype.AIProxyLibrary:
		c.Set(config.KeyLibraryID, channel.Other)
	case channeltype.Ali:
		c.Set(config.KeyPlugin, channel.Other)
	}
	cfg, _ := channel.LoadConfig()
	for k, v := range cfg {
		c.Set(config.KeyPrefix+k, v)
	}
}
