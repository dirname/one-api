package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"one-api/common"
	"one-api/model"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	if common.RelayTimeout == 0 {
		httpClient = &http.Client{}
	} else {
		httpClient = &http.Client{
			Timeout: time.Duration(common.RelayTimeout) * time.Second,
		}
	}

	impatientHTTPClient = &http.Client{
		Timeout: 5 * time.Second,
	}
}

func relayVisionHelper(c *gin.Context, relayMode int) *OpenAIErrorWithStatusCode {
	channelType := c.GetInt("channel")
	channelId := c.GetInt("channel_id")
	tokenId := c.GetInt("token_id")
	userId := c.GetInt("id")
	consumeQuota := c.GetBool("consume_quota")
	group := c.GetString("group")
	var textRequest VisionOpenAIRequest
	if consumeQuota || channelType == common.ChannelTypeAzure || channelType == common.ChannelTypePaLM {
		err := common.UnmarshalBodyReusable(c, &textRequest)
		if err != nil {
			return errorWrapper(err, "bind_request_body_failed", http.StatusBadRequest)
		}
	}
	// request validation
	if textRequest.Model == "" {
		return errorWrapper(errors.New("model is required"), "required_field_missing", http.StatusBadRequest)
	}
	if textRequest.Messages == nil || len(textRequest.Messages) == 0 {
		return errorWrapper(errors.New("field messages is required"), "required_field_missing", http.StatusBadRequest)
	}
	// map model name
	modelMapping := c.GetString("model_mapping")
	isModelMapped := false
	if modelMapping != "" && modelMapping != "{}" {
		modelMap := make(map[string]string)
		err := json.Unmarshal([]byte(modelMapping), &modelMap)
		if err != nil {
			return errorWrapper(err, "unmarshal_model_mapping_failed", http.StatusInternalServerError)
		}
		if modelMap[textRequest.Model] != "" {
			textRequest.Model = modelMap[textRequest.Model]
			isModelMapped = true
		}
	}
	baseURL := common.ChannelBaseURLs[channelType]
	requestURL := c.Request.URL.String()
	if c.GetString("base_url") != "" {
		baseURL = c.GetString("base_url")
	}
	fullRequestURL := getFullRequestURL(baseURL, requestURL, channelType)
	if channelType == common.ChannelTypeAzure {
		// https://learn.microsoft.com/en-us/azure/cognitive-services/openai/chatgpt-quickstart?pivots=rest-api&tabs=command-line#rest-api
		query := c.Request.URL.Query()
		apiVersion := query.Get("api-version")
		if apiVersion == "" {
			apiVersion = c.GetString("api_version")
		}
		requestURL := strings.Split(requestURL, "?")[0]
		requestURL = fmt.Sprintf("%s?api-version=%s", requestURL, apiVersion)
		baseURL = c.GetString("base_url")
		task := strings.TrimPrefix(requestURL, "/v1/")
		model_ := textRequest.Model
		model_ = strings.Replace(model_, ".", "", -1)
		// https://github.com/songquanpeng/one-api/issues/67
		model_ = strings.TrimSuffix(model_, "-0301")
		model_ = strings.TrimSuffix(model_, "-0314")
		model_ = strings.TrimSuffix(model_, "-0613")
		fullRequestURL = fmt.Sprintf("%s/openai/deployments/%s/%s", baseURL, model_, task)
	}
	var promptTokens int
	var completionTokens int
	promptTokens, err := countVisionTokenMessage(textRequest.Messages)
	if err != nil {
		return errorWrapper(err, "count_prompt_tokens_failed", http.StatusBadRequest)
	}
	preConsumedTokens := common.PreConsumedQuota
	if textRequest.MaxTokens != 0 {
		preConsumedTokens = promptTokens + textRequest.MaxTokens
	}
	modelRatio := common.GetModelRatio(textRequest.Model)
	groupRatio := common.GetGroupRatio(group)
	ratio := modelRatio * groupRatio
	preConsumedQuota := int(float64(preConsumedTokens) * ratio)
	userQuota, err := model.CacheGetUserQuota(userId)
	if err != nil {
		return errorWrapper(err, "get_user_quota_failed", http.StatusInternalServerError)
	}
	if userQuota-preConsumedQuota < 0 {
		return errorWrapper(errors.New("user quota is not enough"), "insufficient_user_quota", http.StatusForbidden)
	}
	err = model.CacheDecreaseUserQuota(userId, preConsumedQuota)
	if err != nil {
		return errorWrapper(err, "decrease_user_quota_failed", http.StatusInternalServerError)
	}
	if userQuota > 100*preConsumedQuota {
		// in this case, we do not pre-consume quota
		// because the user has enough quota
		preConsumedQuota = 0
		common.LogInfo(c.Request.Context(), fmt.Sprintf("user %d has enough quota %d, trusted and no need to pre-consume", userId, userQuota))
	}
	if consumeQuota && preConsumedQuota > 0 {
		err := model.PreConsumeTokenQuota(tokenId, preConsumedQuota)
		if err != nil {
			return errorWrapper(err, "pre_consume_token_quota_failed", http.StatusForbidden)
		}
	}
	var requestBody io.Reader
	if isModelMapped {
		jsonStr, err := json.Marshal(textRequest)
		if err != nil {
			return errorWrapper(err, "marshal_text_request_failed", http.StatusInternalServerError)
		}
		requestBody = bytes.NewBuffer(jsonStr)
	} else {
		requestBody = c.Request.Body
	}

	var req *http.Request
	var resp *http.Response
	isStream := textRequest.Stream

	req, err = http.NewRequest(c.Request.Method, fullRequestURL, requestBody)
	if err != nil {
		return errorWrapper(err, "new_request_failed", http.StatusInternalServerError)
	}
	apiKey := c.Request.Header.Get("Authorization")
	apiKey = strings.TrimPrefix(apiKey, "Bearer ")
	if channelType == common.ChannelTypeAzure {
		req.Header.Set("api-key", apiKey)
	} else {
		req.Header.Set("Authorization", c.Request.Header.Get("Authorization"))
		if channelType == common.ChannelTypeOpenRouter {
			req.Header.Set("HTTP-Referer", "https://github.com/songquanpeng/one-api")
			req.Header.Set("X-Title", "One API")
		}
	}
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	req.Header.Set("Accept", c.Request.Header.Get("Accept"))
	if isStream && c.Request.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "text/event-stream")
	}
	//req.Header.Set("Connection", c.Request.Header.Get("Connection"))
	resp, err = httpClient.Do(req)
	if err != nil {
		return errorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}
	err = req.Body.Close()
	if err != nil {
		return errorWrapper(err, "close_request_body_failed", http.StatusInternalServerError)
	}
	err = c.Request.Body.Close()
	if err != nil {
		return errorWrapper(err, "close_request_body_failed", http.StatusInternalServerError)
	}
	isStream = isStream || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream")

	if resp.StatusCode != http.StatusOK {
		if preConsumedQuota != 0 {
			go func(ctx context.Context) {
				// return pre-consumed quota
				err := model.PostConsumeTokenQuota(tokenId, -preConsumedQuota)
				if err != nil {
					common.LogError(ctx, "error return pre-consumed quota: "+err.Error())
				}
			}(c.Request.Context())
		}
		return relayErrorHandler(resp)
	}

	var textResponse TextResponse
	tokenName := c.GetString("token_name")

	defer func(ctx context.Context) {
		// c.Writer.Flush()
		go func() {
			if consumeQuota {
				quota := 0
				completionRatio := common.GetCompletionRatio(textRequest.Model)
				promptTokens = textResponse.Usage.PromptTokens
				completionTokens = textResponse.Usage.CompletionTokens
				quota = int(math.Ceil((float64(promptTokens) + float64(completionTokens)*completionRatio) * ratio))
				if ratio != 0 && quota <= 0 {
					quota = 1
				}
				totalTokens := promptTokens + completionTokens
				if totalTokens == 0 {
					// in this case, must be some error happened
					// we cannot just return, because we may have to return the pre-consumed quota
					quota = 0
				}
				quotaDelta := quota - preConsumedQuota
				err := model.PostConsumeTokenQuota(tokenId, quotaDelta)
				if err != nil {
					common.LogError(ctx, "error consuming token remain quota: "+err.Error())
				}
				err = model.CacheUpdateUserQuota(userId)
				if err != nil {
					common.LogError(ctx, "error update user quota cache: "+err.Error())
				}
				if quota != 0 {
					logContent := fmt.Sprintf("Model multiplier %.2f, basic multiplier %.2f", modelRatio, groupRatio)
					model.RecordConsumeLog(ctx, userId, channelId, promptTokens, completionTokens, textRequest.Model, tokenName, quota, logContent)
					model.UpdateUserUsedQuotaAndRequestCount(userId, quota)
					model.UpdateChannelUsedQuota(channelId, quota)
				}
			}
		}()
	}(c.Request.Context())
	if isStream {
		err, responseText := openaiStreamHandler(c, resp, relayMode)
		if err != nil {
			return err
		}
		textResponse.Usage.PromptTokens = promptTokens
		textResponse.Usage.CompletionTokens = countTokenText(responseText, textRequest.Model)
		return nil
	} else {
		err, usage := openaiHandler(c, resp, consumeQuota, promptTokens, textRequest.Model)
		if err != nil {
			return err
		}
		if usage != nil {
			textResponse.Usage = *usage
		}
		return nil
	}
}
