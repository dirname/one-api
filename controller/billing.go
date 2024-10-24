package controller

import (
	"github.com/gin-gonic/gin"
	"one-api/common"
	"one-api/model"
	"time"
)

func GetSubscription(c *gin.Context) {
	var remainQuota int
	var usedQuota int
	var err error
	var token *model.Token
	var expiredTime int64
	if common.DisplayTokenStatEnabled {
		tokenId := c.GetInt("token_id")
		token, err = model.GetTokenById(tokenId)
		expiredTime = token.ExpiredTime
		remainQuota = token.RemainQuota
		usedQuota = token.UsedQuota
	} else {
		userId := c.GetInt("id")
		remainQuota, err = model.GetUserQuota(userId)
		usedQuota, err = model.GetUserUsedQuota(userId)
	}
	if expiredTime <= 0 {
		expiredTime = 0
	}
	if err != nil {
		openAIError := OpenAIError{
			Message: err.Error(),
			Type:    "upstream_error",
		}
		c.JSON(200, gin.H{
			"error": openAIError,
		})
		return
	}
	quota := remainQuota + usedQuota
	amount := float64(quota)
	if common.DisplayInCurrencyEnabled {
		amount /= common.QuotaPerUnit
	}
	if token != nil && token.UnlimitedQuota {
		amount = 100000000
	}
	subscription := OpenAISubscriptionResponse{
		Object:             "billing_subscription",
		HasPaymentMethod:   true,
		SoftLimitUSD:       amount,
		HardLimitUSD:       amount,
		SystemHardLimitUSD: amount,
		AccessUntil:        expiredTime,
	}
	c.JSON(200, subscription)
	return
}

func GetUsage(c *gin.Context) {
	var quota int
	var err error
	//var token *model.Token
	if common.DisplayTokenStatEnabled {
		userId := c.GetInt("id")
		from := c.Query("start_date")
		to := c.Query("end_date")
		var startDate, endDate time.Time
		startDate, err = time.Parse("2006-01-02", from)
		endDate, err = time.Parse("2006-01-02", to)
		quota, err = model.GetPeriodQuotaSum(userId, startDate.Unix(), endDate.Unix())
		//tokenId := c.GetInt("token_id")
		//token, err = model.GetTokenById(tokenId)
		//quota = token.UsedQuota
	} else {
		userId := c.GetInt("id")
		quota, err = model.GetUserUsedQuota(userId)
	}
	if err != nil {
		openAIError := OpenAIError{
			Message: err.Error(),
			Type:    "PUERHUB_AI_ERROR",
		}
		c.JSON(200, gin.H{
			"error": openAIError,
		})
		return
	}
	amount := float64(quota)
	if common.DisplayInCurrencyEnabled {
		amount /= common.QuotaPerUnit
	}
	usage := OpenAIUsageResponse{
		Object:     "list",
		TotalUsage: amount * 100,
	}
	c.JSON(200, usage)
	return
}
