package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/relay"
	"github.com/songquanpeng/one-api/relay/adaptor/openai"
	"github.com/songquanpeng/one-api/relay/apitype"
	"github.com/songquanpeng/one-api/relay/billing"
	billingratio "github.com/songquanpeng/one-api/relay/billing/ratio"
	"github.com/songquanpeng/one-api/relay/channeltype"
	"github.com/songquanpeng/one-api/relay/meta"
	"github.com/songquanpeng/one-api/relay/model"
	"io"
	"net/http"
	"regexp"
	"strings"
)

func RelayTextHelper(c *gin.Context) *model.ErrorWithStatusCode {
	ctx := c.Request.Context()
	meta := meta.GetByContext(c)
	// get & validate textRequest
	textRequest, err := getAndValidateTextRequest(c, meta.Mode)
	if err != nil {
		logger.Errorf(ctx, "getAndValidateTextRequest failed: %s", err.Error())
		return openai.ErrorWrapper(err, "invalid_text_request", http.StatusBadRequest)
	}
	meta.IsStream = textRequest.Stream

	// map model name
	var isModelMapped bool
	meta.OriginModelName = textRequest.Model
	textRequest.Model, isModelMapped = getMappedModelName(textRequest.Model, meta.ModelMapping)
	meta.ActualModelName = textRequest.Model
	// get model ratio & group ratio
	modelRatio := billingratio.GetModelRatio(textRequest.Model)
	// Support GPTs
	if regexp.MustCompile(`gpt-4-gizmo-g-[a-zA-Z0-9]{9}`).MatchString(textRequest.Model) {
		modelRatio = billingratio.GetModelRatio("gpt-4-gizmo-*")
	}
	groupRatio := billingratio.GetGroupRatio(meta.Group)
	ratio := modelRatio * groupRatio
	// pre-consume quota
	promptTokens := getPromptTokens(textRequest, meta.Mode)
	meta.PromptTokens = promptTokens
	preConsumedQuota, bizErr := preConsumeQuota(ctx, textRequest, promptTokens, ratio, meta)
	if bizErr != nil {
		logger.Warnf(ctx, "preConsumeQuota failed: %+v", *bizErr)
		return bizErr
	}

	adaptor := relay.GetAdaptor(meta.APIType)
	if adaptor == nil {
		return openai.ErrorWrapper(fmt.Errorf("invalid api type: %d", meta.APIType), "invalid_api_type", http.StatusBadRequest)
	}
	adaptor.Init(meta)

	// get request body
	var requestBody io.Reader
	if meta.APIType == apitype.OpenAI {
		// no need to convert request for openai
		shouldResetRequestBody := isModelMapped || meta.ChannelType == channeltype.Baichuan // frequency_penalty 0 is not acceptable for baichuan
		if shouldResetRequestBody {
			jsonStr, err := json.Marshal(textRequest)
			if err != nil {
				return openai.ErrorWrapper(err, "json_marshal_failed", http.StatusInternalServerError)
			}
			requestBody = bytes.NewBuffer(jsonStr)
		} else {
			requestBody = c.Request.Body
		}
	} else {
		convertedRequest, err := adaptor.ConvertRequest(c, meta.Mode, textRequest)
		if err != nil {
			return openai.ErrorWrapper(err, "convert_request_failed", http.StatusInternalServerError)
		}
		jsonData, err := json.Marshal(convertedRequest)
		if err != nil {
			return openai.ErrorWrapper(err, "json_marshal_failed", http.StatusInternalServerError)
		}
		logger.Debugf(ctx, "converted request: \n%s", string(jsonData))
		requestBody = bytes.NewBuffer(jsonData)
	}

	// do request
	resp, err := adaptor.DoRequest(c, meta, requestBody)
	if err != nil {
		logger.Errorf(ctx, "DoRequest failed: %s", err.Error())
		return openai.ErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}
	if isErrorHappened(meta, resp) {
		billing.ReturnPreConsumedQuota(ctx, preConsumedQuota, meta.TokenId)
		return RelayErrorHandler(resp)
	}

	for k, v := range resp.Header {
		values := strings.Join(v, ",")
		re := regexp.MustCompile(`([a-zA-Z0-9-]{1,63}\.)+[a-zA-Z]{2,63}`)
		if re.MatchString(values) || re.MatchString(k) {
			resp.Header.Del(k)
		}
	}

	// do response
	usage, respErr := adaptor.DoResponse(c, resp, meta)
	if respErr != nil {
		logger.Errorf(ctx, "respErr is not nil: %+v", respErr)
		billing.ReturnPreConsumedQuota(ctx, preConsumedQuota, meta.TokenId)
		return respErr
	}

	switch strings.ToLower(textRequest.Model) {
	case "suno-v3":
		go postFixedConsumeQuota(ctx, usage, meta, textRequest, ratio, preConsumedQuota, modelRatio)
	case "search-gpts-chat":
		go postFixedConsumeQuota(ctx, usage, meta, textRequest, ratio, preConsumedQuota, modelRatio)
	case "search-gpts":
		go postFixedConsumeQuota(ctx, usage, meta, textRequest, ratio, preConsumedQuota, modelRatio)
	default:
		go postConsumeQuota(ctx, usage, meta, textRequest, ratio, preConsumedQuota, modelRatio, groupRatio)
	}

	// post-consume quota
	//go postConsumeQuota(ctx, usage, meta, textRequest, ratio, preConsumedQuota, modelRatio, groupRatio)
	return nil
}
