package openai

func ErrorWrapper(err error, code string, statusCode int) *ErrorWithStatusCode {
	Error := Error{
		Message: err.Error(),
		Type:    "PUERHUB_AI_ERROR",
		Code:    code,
	}
	return &ErrorWithStatusCode{
		Error:      Error,
		StatusCode: statusCode,
	}
}
