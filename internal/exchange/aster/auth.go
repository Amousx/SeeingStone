package aster

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// Auth Aster 认证模块
type Auth struct {
	APIKey    string
	SecretKey string
}

// NewAuth 创建认证实例
func NewAuth(apiKey, secretKey string) *Auth {
	return &Auth{
		APIKey:    apiKey,
		SecretKey: secretKey,
	}
}

// SignRequest 签名请求
// 根据Aster API文档，签名使用 HMAC SHA256
func (a *Auth) SignRequest(params map[string]string) string {
	// 添加时间戳
	if _, exists := params["timestamp"]; !exists {
		params["timestamp"] = strconv.FormatInt(time.Now().UnixMilli(), 10)
	}

	// 排序参数并构建查询字符串
	queryString := a.buildQueryString(params)

	// 使用 HMAC SHA256 签名
	h := hmac.New(sha256.New, []byte(a.SecretKey))
	h.Write([]byte(queryString))
	signature := hex.EncodeToString(h.Sum(nil))

	return signature
}

// buildQueryString 构建查询字符串
func (a *Auth) buildQueryString(params map[string]string) string {
	// 获取所有键并排序
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 构建查询字符串
	values := url.Values{}
	for _, k := range keys {
		values.Add(k, params[k])
	}

	return values.Encode()
}

// AddAuthHeaders 添加认证头
func (a *Auth) AddAuthHeaders(headers map[string]string) {
	headers["X-MBX-APIKEY"] = a.APIKey
}

// SignedParams 为参数添加签名
func (a *Auth) SignedParams(params map[string]string) map[string]string {
	if params == nil {
		params = make(map[string]string)
	}

	// 添加时间戳
	params["timestamp"] = strconv.FormatInt(time.Now().UnixMilli(), 10)

	// 添加接收窗口（推荐5秒）
	if _, exists := params["recvWindow"]; !exists {
		params["recvWindow"] = "5000"
	}

	// 生成签名
	signature := a.SignRequest(params)
	params["signature"] = signature

	return params
}

// GetTimestamp 获取当前时间戳（毫秒）
func (a *Auth) GetTimestamp() int64 {
	return time.Now().UnixMilli()
}

// ValidateTimestamp 验证时间戳是否在有效窗口内
func (a *Auth) ValidateTimestamp(timestamp int64, recvWindow int64) error {
	serverTime := time.Now().UnixMilli()

	// 检查时间戳是否在未来
	if timestamp > serverTime+1000 {
		return fmt.Errorf("timestamp is in the future")
	}

	// 检查时间戳是否过期
	if serverTime-timestamp > recvWindow {
		return fmt.Errorf("timestamp is too old")
	}

	return nil
}
