package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chai2010/webp"
	"github.com/gin-gonic/gin"
	"github.com/pkoukk/tiktoken-go"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"one-api/common"
	"one-api/model"
	"strconv"
	"strings"
)

var stopFinishReason = "stop"

// tokenEncoderMap won't grow after initialization
var tokenEncoderMap = map[string]*tiktoken.Tiktoken{}
var defaultTokenEncoder *tiktoken.Tiktoken

func InitTokenEncoders() {
	common.SysLog("initializing token encoders")
	gpt35TokenEncoder, err := tiktoken.EncodingForModel("gpt-3.5-turbo")
	if err != nil {
		common.FatalLog(fmt.Sprintf("failed to get gpt-3.5-turbo token encoder: %s", err.Error()))
	}
	defaultTokenEncoder = gpt35TokenEncoder
	gpt4TokenEncoder, err := tiktoken.EncodingForModel("gpt-4")
	if err != nil {
		common.FatalLog(fmt.Sprintf("failed to get gpt-4 token encoder: %s", err.Error()))
	}
	for model, _ := range common.ModelRatio {
		if strings.HasPrefix(model, "gpt-3.5") {
			tokenEncoderMap[model] = gpt35TokenEncoder
		} else if strings.HasPrefix(model, "gpt-4") {
			tokenEncoderMap[model] = gpt4TokenEncoder
		} else {
			tokenEncoderMap[model] = nil
		}
	}
	common.SysLog("token encoders initialized")
}

func getTokenEncoder(model string) *tiktoken.Tiktoken {
	tokenEncoder, ok := tokenEncoderMap[model]
	if ok && tokenEncoder != nil {
		return tokenEncoder
	}
	if ok {
		tokenEncoder, err := tiktoken.EncodingForModel(model)
		if err != nil {
			common.SysError(fmt.Sprintf("failed to get token encoder for model %s: %s, using encoder for gpt-3.5-turbo", model, err.Error()))
			tokenEncoder = defaultTokenEncoder
		}
		tokenEncoderMap[model] = tokenEncoder
		return tokenEncoder
	}
	return defaultTokenEncoder
}

func getTokenNum(tokenEncoder *tiktoken.Tiktoken, text string) int {
	if common.ApproximateTokenEnabled {
		return int(float64(len(text)) * 0.38)
	}
	return len(tokenEncoder.Encode(text, nil, nil))
}

func countTokenMessages(messages []Message, model string) int {
	tokenEncoder := getTokenEncoder(model)
	// Reference:
	// https://github.com/openai/openai-cookbook/blob/main/examples/How_to_count_tokens_with_tiktoken.ipynb
	// https://github.com/pkoukk/tiktoken-go/issues/6
	//
	// Every message follows <|start|>{role/name}\n{content}<|end|>\n
	var tokensPerMessage int
	var tokensPerName int
	if model == "gpt-3.5-turbo-0301" {
		tokensPerMessage = 4
		tokensPerName = -1 // If there's a name, the role is omitted
	} else {
		tokensPerMessage = 3
		tokensPerName = 1
	}
	tokenNum := 0
	for _, message := range messages {
		tokenNum += tokensPerMessage
		tokenNum += getTokenNum(tokenEncoder, message.Content)
		tokenNum += getTokenNum(tokenEncoder, message.Role)
		if message.Name != nil {
			tokenNum += tokensPerName
			tokenNum += getTokenNum(tokenEncoder, *message.Name)
		}
	}
	tokenNum += 3 // Every reply is primed with <|start|>assistant<|message|>
	return tokenNum
}

type imgInfo struct {
	width  int
	height int
	detail string
}

func (image imgInfo) calculateTokenCost() int {
	if image.detail == "low" {
		return 85
	}

	maxSize := 2048
	minSize := 768
	tileSize := 512
	tileCost := 170
	baseCost := 85

	// Scale down to fit within 2048 square if necessary
	if image.width > maxSize || image.height > maxSize {
		ratio := float64(image.width) / float64(image.height)
		if image.width > image.height {
			image.width = maxSize
			image.height = int(float64(maxSize) / ratio)
		} else {
			image.height = maxSize
			image.width = int(float64(maxSize) * ratio)
		}
	}

	// Scale down to shortest side is 768px
	if image.width > minSize || image.height > minSize {
		if image.width < image.height {
			image.height = int(float64(image.height) / float64(image.width) * float64(minSize))
			image.width = minSize
		} else {
			image.width = int(float64(image.width) / float64(image.height) * float64(minSize))
			image.height = minSize
		}
	}

	// Calculate number of 512px tiles needed
	tiles := math.Ceil(float64(image.width)/float64(tileSize)) * math.Ceil(float64(image.height)/float64(tileSize))

	// Calculate final token cost
	return int(tiles)*tileCost + baseCost
}

func getImageInfo(url string) (*imgInfo, error) {
	var isWebp bool
	var data []byte
	var err error
	switch {
	case strings.HasPrefix(url, "data:image/"):
		isWebp, data, err = getImageFromBase64(url)
		break
	case strings.HasPrefix(url, "http"):
		isWebp, data, err = getImageFromURL(url)
		break
	default:
		return nil, errors.New("invalid image url")
	}

	res := &imgInfo{}
	if isWebp {
		w, h, _, err := webp.GetInfo(data)
		if err != nil {
			return nil, errors.New("failed to get webp image info")
		}
		res.width = w
		res.height = h
		return res, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("failed to decode image")
	}
	res.width = img.Bounds().Dx()
	res.height = img.Bounds().Dy()
	return res, nil
}

func getImageFromURL(url string) (isWebp bool, resp []byte, err error) {
	// Decode image
	res, err := http.Get(url)
	if err != nil {
		err = errors.New("failed to get image")
		return
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(res.Body)

	resp, err = io.ReadAll(res.Body)

	contentType := res.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "image/webp") {
		isWebp = true
	}

	return
}

func getImageFromBase64(base64Data string) (isWebp bool, resp []byte, err error) {
	// Remove data:image/xxx;base64, prefix
	isWebp = strings.HasPrefix(base64Data, "image/webp")
	data := strings.SplitN(base64Data, ",", 2)
	if len(data) != 2 {
		err = errors.New("invalid base64 data")
		return
	}

	// Decode base64 data
	resp, err = base64.StdEncoding.DecodeString(data[1])
	if err != nil {
		err = errors.New("failed to decoding base64 data")
		return
	}

	return
}

func countVisionTokenMessage(messages []VisionMessage) (int, error) {
	tokenEncoder := getTokenEncoder("gpt-4")
	var tokensPerMessage int
	var tokensPerName int
	tokensPerMessage = 3
	tokensPerName = 1
	tokenNum := 0
	for _, message := range messages {
		tokenNum += tokensPerMessage
		tokenNum += getTokenNum(tokenEncoder, message.Role)
		if message.Name != nil {
			tokenNum += tokensPerName
			tokenNum += getTokenNum(tokenEncoder, *message.Name)
		}

		content, err := message.Content.MarshalJSON()
		if err != nil {
			return 0, fmt.Errorf("failed to marshal content")
		}

		visions := make([]VisionContent, 0)
		err = json.Unmarshal(content, &visions)

		if err == nil {
			for _, content := range visions {
				tokenNum += getTokenNum(tokenEncoder, content.Text)
				if len(content.ImageURL.URL) > 0 {
					img, err := getImageInfo(content.ImageURL.URL)
					if err != nil {
						return 0, fmt.Errorf("failed to get %s info", content.ImageURL.URL)
					}
					detail := "auto"
					if len(content.ImageURL.Detail) > 0 {
						detail = content.ImageURL.Detail
					}
					img.detail = detail

					tokenNum += img.calculateTokenCost()
				}
			}
		} else {
			tokenNum += getTokenNum(tokenEncoder, string(content))
		}
	}
	tokenNum += 3 // Every reply is primed with <|start|>assistant<|message|>
	return tokenNum, nil
}

func countTokenInput(input any, model string) int {
	switch input.(type) {
	case string:
		return countTokenText(input.(string), model)
	case []string:
		text := ""
		for _, s := range input.([]string) {
			text += s
		}
		return countTokenText(text, model)
	}
	return 0
}

func countTokenText(text string, model string) int {
	tokenEncoder := getTokenEncoder(model)
	return getTokenNum(tokenEncoder, text)
}

func errorWrapper(err error, code string, statusCode int) *OpenAIErrorWithStatusCode {
	openAIError := OpenAIError{
		Message: err.Error(),
		Type:    "PUERHUB_AI_ERROR",
		Code:    code,
	}
	return &OpenAIErrorWithStatusCode{
		OpenAIError: openAIError,
		StatusCode:  statusCode,
	}
}

func shouldDisableChannel(err *OpenAIError, statusCode int) bool {
	if !common.AutomaticDisableChannelEnabled {
		return false
	}
	if err == nil {
		return false
	}
	if statusCode == http.StatusUnauthorized {
		return true
	}
	if err.Type == "insufficient_quota" || err.Code == "invalid_api_key" || err.Code == "account_deactivated" {
		return true
	}
	return false
}

func setEventStreamHeaders(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
}

func relayErrorHandler(resp *http.Response) (openAIErrorWithStatusCode *OpenAIErrorWithStatusCode) {
	openAIErrorWithStatusCode = &OpenAIErrorWithStatusCode{
		StatusCode: resp.StatusCode,
		OpenAIError: OpenAIError{
			Message: fmt.Sprintf("bad response status code %d", resp.StatusCode),
			Type:    "upstream_error",
			Code:    "bad_response_status_code",
			Param:   strconv.Itoa(resp.StatusCode),
		},
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	err = resp.Body.Close()
	if err != nil {
		return
	}
	var textResponse TextResponse
	err = json.Unmarshal(responseBody, &textResponse)
	if err != nil {
		return
	}
	openAIErrorWithStatusCode.OpenAIError = textResponse.Error
	return
}

func getFullRequestURL(baseURL string, requestURL string, channelType int) string {
	fullRequestURL := fmt.Sprintf("%s%s", baseURL, requestURL)

	if strings.HasPrefix(baseURL, "https://gateway.ai.cloudflare.com") {
		switch channelType {
		case common.ChannelTypeOpenAI:
			fullRequestURL = fmt.Sprintf("%s%s", baseURL, strings.TrimPrefix(requestURL, "/v1"))
		case common.ChannelTypeAzure:
			fullRequestURL = fmt.Sprintf("%s%s", baseURL, strings.TrimPrefix(requestURL, "/openai/deployments"))
		}
	}

	return fullRequestURL
}

func postConsumeQuota(ctx context.Context, tokenId int, quotaDelta int, totalQuota int, userId int, channelId int, modelRatio float64, groupRatio float64, modelName string, tokenName string) {
	// quotaDelta is remaining quota to be consumed
	err := model.PostConsumeTokenQuota(tokenId, quotaDelta)
	if err != nil {
		common.SysError("error consuming token remain quota: " + err.Error())
	}
	err = model.CacheUpdateUserQuota(userId)
	if err != nil {
		common.SysError("error update user quota cache: " + err.Error())
	}
	// totalQuota is total quota consumed
	if totalQuota != 0 {
		logContent := fmt.Sprintf("Model multiplier %.2f, basic multiplier %.2f", modelRatio, groupRatio)
		model.RecordConsumeLog(ctx, userId, channelId, totalQuota, 0, modelName, tokenName, totalQuota, logContent)
		model.UpdateUserUsedQuotaAndRequestCount(userId, totalQuota)
		model.UpdateChannelUsedQuota(channelId, totalQuota)
	}
	if totalQuota <= 0 {
		common.LogError(ctx, fmt.Sprintf("totalQuota consumed is %d, something is wrong", totalQuota))
	}
}
