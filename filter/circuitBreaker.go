package filter

import (
	"errors"
	"strconv"

	"github.com/afex/hystrix-go/hystrix"
	motan "github.com/weibocom/motan-go/core"
	"github.com/weibocom/motan-go/log"
)

const (
	RequestVolumeThresholdField = "circuitBreaker.requestThreshold"
	SleepWindowField            = "circuitBreaker.sleepWindow"  //ms
	ErrorPercentThreshold       = "circuitBreaker.errorPercent" //%
	IncludeBizException         = "circuitBreaker.bizException"
)

type CircuitBreakerFilter struct {
	url                 *motan.URL
	next                motan.EndPointFilter
	circuitBreaker      *hystrix.CircuitBreaker
	includeBizException bool
}

func (c *CircuitBreakerFilter) GetIndex() int {
	return 20
}

func (c *CircuitBreakerFilter) GetName() string {
	return CircuitBreaker
}

func (c *CircuitBreakerFilter) NewFilter(url *motan.URL) motan.Filter {
	bizException := newCircuitBreaker(c.GetName(), url)
	return &CircuitBreakerFilter{url: url, includeBizException: bizException}
}

func (c *CircuitBreakerFilter) Filter(caller motan.Caller, request motan.Request) motan.Response {
	var response motan.Response
	err := hystrix.Do(c.url.GetIdentity(), func() error {
		response = c.GetNext().Filter(caller, request)
		return checkException(response, c.includeBizException)
	}, nil)
	if err != nil {
		return defaultErrMotanResponse(request, err.Error())
	}
	return response
}

func (c *CircuitBreakerFilter) HasNext() bool {
	return c.next != nil
}

func (c *CircuitBreakerFilter) SetNext(nextFilter motan.EndPointFilter) {
	c.next = nextFilter
}

func (c *CircuitBreakerFilter) GetNext() motan.EndPointFilter {
	return c.next
}

func (c *CircuitBreakerFilter) GetType() int32 {
	return motan.EndPointFilterType
}

func newCircuitBreaker(filterName string, url *motan.URL) bool {
	bizExceptionStr := url.GetParam(IncludeBizException, "true")
	bizException, err := strconv.ParseBool(bizExceptionStr)
	if err != nil {
		bizException = true
		vlog.Warningf("[%s] parse config %s error, use default", filterName, IncludeBizException)
	}
	commandConfig := buildCommandConfig(filterName, url)
	hystrix.ConfigureCommand(url.GetIdentity(), *commandConfig)
	if _, _, err = hystrix.GetCircuit(url.GetIdentity()); err != nil {
		vlog.Errorf("[%s] new circuit fail. err:%s, url:%v, config{%s}\n", err.Error(), filterName, url.GetIdentity(), getConfigStr(commandConfig)+"bizException:"+bizExceptionStr)
	} else {
		vlog.Infof("[%s] new circuit success. url:%v, config{%s}\n", filterName, url.GetIdentity(), getConfigStr(commandConfig)+"bizException:"+bizExceptionStr)
	}
	return bizException
}

func buildCommandConfig(filterName string, url *motan.URL) *hystrix.CommandConfig {
	hystrix.DefaultMaxConcurrent = 1000
	hystrix.DefaultTimeout = int(url.GetPositiveIntValue(motan.TimeOutKey, int64(hystrix.DefaultTimeout))) * 2
	commandConfig := &hystrix.CommandConfig{}
	if v, ok := url.Parameters[RequestVolumeThresholdField]; ok {
		if temp, _ := strconv.Atoi(v); temp > 0 {
			commandConfig.RequestVolumeThreshold = temp
		} else {
			vlog.Warningf("[%s] parse config %s error, use default", filterName, RequestVolumeThresholdField)
		}
	}
	if v, ok := url.Parameters[SleepWindowField]; ok {
		if temp, _ := strconv.Atoi(v); temp > 0 {
			commandConfig.SleepWindow = temp
		} else {
			vlog.Warningf("[%s] parse config %s error, use default", filterName, SleepWindowField)
		}
	}
	if v, ok := url.Parameters[ErrorPercentThreshold]; ok {
		if temp, _ := strconv.Atoi(v); temp > 0 && temp <= 100 {
			commandConfig.ErrorPercentThreshold = temp
		} else {
			vlog.Warningf("[%s] parse config %s error, use default", filterName, ErrorPercentThreshold)
		}
	}
	return commandConfig
}

func defaultErrMotanResponse(request motan.Request, errMsg string) motan.Response {
	response := &motan.MotanResponse{
		RequestID:   request.GetRequestID(),
		Attachment:  motan.NewStringMap(motan.DefaultAttachmentSize),
		ProcessTime: 0,
		Exception: &motan.Exception{
			ErrCode: 400,
			ErrMsg:  errMsg,
			ErrType: motan.ServiceException},
	}
	return response
}

func getConfigStr(config *hystrix.CommandConfig) string {
	var ret string
	if config.RequestVolumeThreshold != 0 {
		ret += "requestThreshold:" + strconv.Itoa(config.RequestVolumeThreshold) + " "
	} else {
		ret += "requestThreshold:" + strconv.Itoa(hystrix.DefaultVolumeThreshold) + " "
	}
	if config.SleepWindow != 0 {
		ret += "sleepWindow:" + strconv.Itoa(config.SleepWindow) + " "
	} else {
		ret += "sleepWindow:" + strconv.Itoa(hystrix.DefaultSleepWindow) + " "
	}
	if config.ErrorPercentThreshold != 0 {
		ret += "errorPercent:" + strconv.Itoa(config.ErrorPercentThreshold) + " "
	} else {
		ret += "errorPercent:" + strconv.Itoa(hystrix.DefaultErrorPercentThreshold) + " "
	}
	return ret
}

func checkException(response motan.Response, includeBizException bool) error {
	if ex := response.GetException(); ex != nil && (includeBizException || ex.ErrType != motan.BizException) {
		return errors.New(ex.ErrMsg)
	}
	return nil
}
