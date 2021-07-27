package awssetup

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/cresta/zapctx"
	"go.uber.org/zap"
)

func CreateSession(ctx context.Context, logger *zapctx.Logger, client *http.Client) (*session.Session, error) {
	logger.Info(ctx, "<-CreateSession")
	defer logger.Info(ctx, "->CreateSession")
	awsConfig := aws.NewConfig()
	awsConfig.CredentialsChainVerboseErrors = aws.Bool(true)
	awsConfig.Logger = aws.LoggerFunc(func(i ...interface{}) {
		logger.Warn(context.Background(), "aws log output", zap.Any("keys", i))
	})
	awsConfig.HTTPClient = client
	ret, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to make AWS session: %w", err)
	}
	s := sts.New(ret)
	out, err := s.GetCallerIdentityWithContext(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("unable to verify caller identity: %w", err)
	}
	logger.Info(ctx, "AWS client identity", zap.Stringp("arn", out.Arn), zap.Stringp("account", out.Account), zap.Stringp("user_id", out.UserId))
	return ret, nil
}
