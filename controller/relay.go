package controller

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/helper"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/model"
	"github.com/songquanpeng/one-api/relay/channel/openai"
	"github.com/songquanpeng/one-api/relay/constant"
	"github.com/songquanpeng/one-api/relay/controller"
	"github.com/songquanpeng/one-api/relay/util"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// https://platform.openai.com/docs/api-reference/chat

func Relay(c *gin.Context) {
	relayMode := constant.Path2RelayMode(c.Request.URL.Path)
	var err *openai.ErrorWithStatusCode
	switch relayMode {
	case constant.RelayModeImagesGenerations:
		err = controller.RelayImageHelper(c, relayMode)
	case constant.RelayModeAudioSpeech:
		fallthrough
	case constant.RelayModeAudioTranslation:
		fallthrough
	case constant.RelayModeAudioTranscription:
		err = controller.RelayAudioHelper(c, relayMode)
	default:
		err = controller.RelayTextHelper(c)
	}
	if err != nil {
		channelId := c.GetInt("channel_id")
		channel, _err := model.GetChannelById(channelId, false)
		if _err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Not found for channel",
			})
		}
		baseURL := channel.GetBaseURL()
		requestId := c.GetString(logger.RequestIdKey)
		retryTimesStr := c.Query("retry")
		retryTimes, _ := strconv.Atoi(retryTimesStr)
		if retryTimesStr == "" {
			retryTimes = config.RetryTimes
		}
		if retryTimes > 0 {
			c.Redirect(http.StatusTemporaryRedirect, fmt.Sprintf("%s?retry=%d", c.Request.URL.Path, retryTimes-1))
		} else {
			if err.StatusCode == http.StatusTooManyRequests {
				err.Error.Message = "The current service node is overloaded. Please try again later."
			}
			err.Error.Message = helper.MessageWithRequestId(replaceUpstreamInfo(err.Error.Message, baseURL, channelId, channel.Type, false), requestId)
			err.Error.Type = replaceUpstreamInfo(err.Error.Type, baseURL, channelId, channel.Type, true)
			c.JSON(err.StatusCode, gin.H{
				"error": err.Error,
			})
		}
		logger.Error(c.Request.Context(), fmt.Sprintf("relay error (channel #%d): %s", channelId, err.Message))
		// https://platform.openai.com/docs/guides/error-codes/api-errors
		if util.ShouldDisableChannel(&err.Error, err.StatusCode) {
			channelId := c.GetInt("channel_id")
			channelName := c.GetString("channel_name")
			disableChannel(channelId, channelName, err.Message)
		}
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := openai.Error{
		Message: "API not implemented",
		Type:    "PUERHUB_AI_ERROR",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := openai.Error{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func replaceUpstreamInfo(info, base string, id, channelType int, isType bool) string {
	pattern := regexp.MustCompile(`//([\w.-]+)`)
	base = strings.ReplaceAll(pattern.FindString(base), `//`, "")
	key := []string{"one-api", "one_api", "ONE_API", "ONE-API", "shell-api", "shell_api", "SHELL_API", "SHELL-API"}
	if len(base) > 0 {
		key = append(key, base)
	}
	replace := common.ChannelBaseURLs[channelType]
	replace = strings.ReplaceAll(replace, "https://", "")
	if isType {
		replace = fmt.Sprintf("[%d] upstream", id)
	}
	for _, k := range key {
		info = strings.ReplaceAll(info, k, replace)
	}

	re := regexp.MustCompile(`当前分组 (.*?) 下对于模型 (.*?) `)
	match := re.FindStringSubmatch(info)

	if len(match) > 0 {
		info = fmt.Sprintf("The model '%s' under the current service node is unavailable; please switch to a different model", match[2])
	}

	re = regexp.MustCompile(`( )?\(request id: [^\)]+\)( )?`)
	info = re.ReplaceAllString(info, "")

	return info
}
