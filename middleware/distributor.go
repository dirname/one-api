package middleware

import (
	"fmt"
	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/model"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type ModelRequest struct {
	Model string `json:"model"`
}

func Distribute() func(c *gin.Context) {
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		userGroup, _ := model.CacheGetUserGroup(userId)
		c.Set("group", userGroup)
		var channel *model.Channel
		channelId, ok := c.Get("channelId")
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
			if channel.Status != common.ChannelStatusEnabled {
				abortWithMessage(c, http.StatusForbidden, "This service node has been disabled")
				return
			}
		} else {
			// Select a channel for the user
			var modelRequest ModelRequest
			err := common.UnmarshalBodyReusable(c, &modelRequest)
			if err != nil {
				abortWithMessage(c, http.StatusBadRequest, "Invalid request")
				return
			}
			if strings.HasPrefix(c.Request.URL.Path, "/v1/moderations") {
				if modelRequest.Model == "" {
					modelRequest.Model = "text-moderation-stable"
				}
			}
			if strings.HasSuffix(c.Request.URL.Path, "embeddings") {
				if modelRequest.Model == "" {
					modelRequest.Model = c.Param("model")
				}
			}
			if strings.HasPrefix(c.Request.URL.Path, "/v1/images/generations") {
				if modelRequest.Model == "" {
					modelRequest.Model = "dall-e-2"
				}
			}
			if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/transcriptions") || strings.HasPrefix(c.Request.URL.Path, "/v1/audio/translations") {
				if modelRequest.Model == "" {
					modelRequest.Model = "whisper-1"
				}
			}
			// Support GPTs
			re := regexp.MustCompile(`gpt-4-gizmo-g-[a-zA-Z0-9]{9}`)
			if modelRequest.Model == "gpt-4-gizmo-*" || modelRequest.Model == "gpt-4-gizmo-g" || (strings.HasPrefix(modelRequest.Model, "gpt-4-gizmo-g") && !re.MatchString(modelRequest.Model)) {
				message := "Please specify a specific model, GPTs share links typified by strings such as 'g-xxxxxxxxx', featuring an 11-character string that include 'g-'. The complete model name appears as follows: 'gpt-4-gizmo-g-xxxxxxxxx'."
				abortWithMessage(c, http.StatusServiceUnavailable, message)
				return
			} else if strings.HasPrefix(modelRequest.Model, "gpt-4-gizmo-g") {
				modelRequest.Model = "gpt-4-gizmo-*"
			}
			channel, err = model.CacheGetRandomSatisfiedChannel(userGroup, modelRequest.Model)
			if err != nil {
				message := fmt.Sprintf("No available service nodes for model %s", modelRequest.Model)
				if channel != nil {
					logger.SysError(fmt.Sprintf("Service node does not exist: %d", channel.Id))
					message = "数据库一致性已被破坏，请联系管理员"
				}
				abortWithMessage(c, http.StatusServiceUnavailable, message)
				return
			}
		}
		c.Set("channel", channel.Type)
		c.Set("channel_id", channel.Id)
		c.Set("channel_name", channel.Name)
		c.Set("model_mapping", channel.GetModelMapping())
		c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", channel.Key))
		c.Set("base_url", channel.GetBaseURL())
		switch channel.Type {
		case common.ChannelTypeAzure:
			c.Set("api_version", channel.Other)
		case common.ChannelTypeXunfei:
			c.Set("api_version", channel.Other)
		case common.ChannelTypeGemini:
			c.Set("api_version", channel.Other)
		case common.ChannelTypeAIProxyLibrary:
			c.Set("library_id", channel.Other)
		case common.ChannelTypeAli:
			c.Set("plugin", channel.Other)
		}
		c.Next()
	}
}
