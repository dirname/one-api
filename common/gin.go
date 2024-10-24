package common

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"io"
	"strings"
)

func UnmarshalBodyReusable(c *gin.Context, v any) error {
	requestBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	err = c.Request.Body.Close()
	if err != nil {
		return err
	}
	contentType := c.Request.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		err = json.Unmarshal(requestBody, &v)
	} else {
		// skip for now
		// TODO: someday non json request have variant model, we will need to implementation this
	}
	if err != nil {
		return err
	}
	// Reset request body
	c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
	return nil
}

func UnmarshalBodyIsVersionModel(c *gin.Context) bool {
	requestBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return false
	}
	err = c.Request.Body.Close()
	if err != nil {
		return false
	}

	v := struct {
		Model string `json:"model"`
	}{}

	err = json.Unmarshal(requestBody, &v)
	if err != nil {
		return false
	}
	// Reset request body
	c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))

	if strings.Index(v.Model, "vision") > -1 {
		return true
	}
	return false
}
