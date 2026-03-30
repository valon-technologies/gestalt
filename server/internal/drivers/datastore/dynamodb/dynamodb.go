package dynamodb

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/crypto"
)

const (
	attrPK = "PK"
	attrSK = "SK"

	userPKPrefix     = "USER#"
	emailPKPrefix    = "EMAIL#"
	profileSK        = "PROFILE"
	uniqueEmailSK    = "UNIQUE"
	tokenSKPrefix    = "TOKEN#"
	apiTokenSKPrefix = "APITOKEN#"

	attrID                = "id"
	attrEmail             = "email"
	attrDisplayName       = "display_name"
	attrCreatedAt         = "created_at"
	attrUpdatedAt         = "updated_at"
	attrUserID            = "user_id"
	attrIntegration       = "integration"
	attrConnection        = "connection"
	attrInstance          = "instance"
	attrAccessTokenEnc    = "access_token_enc"
	attrRefreshTokenEnc   = "refresh_token_enc"
	attrScopes            = "scopes"
	attrExpiresAt         = "expires_at"
	attrLastRefreshedAt   = "last_refreshed_at"
	attrRefreshErrorCount = "refresh_error_count"
	attrMetadataJSON      = "metadata_json"
	attrName              = "name"
	attrHashedToken       = "hashed_token"

	gsiEmail       = "email-index"
	gsiID          = "id-index"
	gsiHashedToken = "hashed-token-index"
)

type Config struct {
	Table         string
	Region        string
	Endpoint      string
	EncryptionKey []byte
}

type Store struct {
	client    *dynamodb.Client
	enc       *crypto.AESGCMEncryptor
	tableName string
}

func New(cfg Config) (*Store, error) {
	enc, err := crypto.NewAESGCM(cfg.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("dynamodb: creating encryptor: %w", err)
	}

	var opts []func(*awsconfig.LoadOptions) error
	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.Endpoint != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider("local", "local", ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("dynamodb: loading aws config: %w", err)
	}

	var clientOpts []func(*dynamodb.Options)
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	}

	return &Store{
		client:    dynamodb.NewFromConfig(awsCfg, clientOpts...),
		enc:       enc,
		tableName: cfg.Table,
	}, nil
}

func (s *Store) Ping(ctx context.Context) error {
	_, err := s.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: &s.tableName,
	})
	if err != nil {
		return fmt.Errorf("dynamodb: ping: %w", err)
	}
	return nil
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: &s.tableName,
		KeySchema: []ddbtypes.KeySchemaElement{
			{AttributeName: aws.String(attrPK), KeyType: ddbtypes.KeyTypeHash},
			{AttributeName: aws.String(attrSK), KeyType: ddbtypes.KeyTypeRange},
		},
		AttributeDefinitions: []ddbtypes.AttributeDefinition{
			{AttributeName: aws.String(attrPK), AttributeType: ddbtypes.ScalarAttributeTypeS},
			{AttributeName: aws.String(attrSK), AttributeType: ddbtypes.ScalarAttributeTypeS},
			{AttributeName: aws.String(attrEmail), AttributeType: ddbtypes.ScalarAttributeTypeS},
			{AttributeName: aws.String(attrID), AttributeType: ddbtypes.ScalarAttributeTypeS},
			{AttributeName: aws.String(attrHashedToken), AttributeType: ddbtypes.ScalarAttributeTypeS},
		},
		GlobalSecondaryIndexes: []ddbtypes.GlobalSecondaryIndex{
			{
				IndexName: aws.String(gsiEmail),
				KeySchema: []ddbtypes.KeySchemaElement{
					{AttributeName: aws.String(attrEmail), KeyType: ddbtypes.KeyTypeHash},
				},
				Projection: &ddbtypes.Projection{ProjectionType: ddbtypes.ProjectionTypeAll},
			},
			{
				IndexName: aws.String(gsiID),
				KeySchema: []ddbtypes.KeySchemaElement{
					{AttributeName: aws.String(attrID), KeyType: ddbtypes.KeyTypeHash},
					{AttributeName: aws.String(attrSK), KeyType: ddbtypes.KeyTypeRange},
				},
				Projection: &ddbtypes.Projection{ProjectionType: ddbtypes.ProjectionTypeKeysOnly},
			},
			{
				IndexName: aws.String(gsiHashedToken),
				KeySchema: []ddbtypes.KeySchemaElement{
					{AttributeName: aws.String(attrHashedToken), KeyType: ddbtypes.KeyTypeHash},
				},
				Projection: &ddbtypes.Projection{ProjectionType: ddbtypes.ProjectionTypeAll},
			},
		},
		BillingMode: ddbtypes.BillingModePayPerRequest,
	})
	if err != nil {
		var riue *ddbtypes.ResourceInUseException
		if errors.As(err, &riue) {
			return nil
		}
		return fmt.Errorf("dynamodb: creating table: %w", err)
	}
	return nil
}

func (s *Store) Close() error { return nil }

func (s *Store) GetUser(ctx context.Context, id string) (*core.User, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &s.tableName,
		Key:       userKey(id),
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb: getting user: %w", err)
	}
	if out.Item == nil {
		return nil, core.ErrNotFound
	}
	return unmarshalUser(out.Item)
}

func (s *Store) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	user, err := s.queryUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return user, nil
	}

	now := time.Now().UTC().Truncate(time.Second)
	user = &core.User{
		ID:        uuid.NewString(),
		Email:     email,
		CreatedAt: now,
		UpdatedAt: now,
	}

	cond := expression.AttributeNotExists(expression.Name(attrPK))
	condExpr, err := expression.NewBuilder().WithCondition(cond).Build()
	if err != nil {
		return nil, fmt.Errorf("dynamodb: building condition: %w", err)
	}

	_, err = s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []ddbtypes.TransactWriteItem{
			{
				Put: &ddbtypes.Put{
					TableName: &s.tableName,
					Item:      marshalUser(user),
				},
			},
			{
				Put: &ddbtypes.Put{
					TableName: &s.tableName,
					Item: map[string]ddbtypes.AttributeValue{
						attrPK: &ddbtypes.AttributeValueMemberS{Value: emailPKPrefix + email},
						attrSK: &ddbtypes.AttributeValueMemberS{Value: uniqueEmailSK},
						attrID: &ddbtypes.AttributeValueMemberS{Value: user.ID},
					},
					ConditionExpression:       condExpr.Condition(),
					ExpressionAttributeNames:  condExpr.Names(),
					ExpressionAttributeValues: condExpr.Values(),
				},
			},
		},
	})
	if err != nil {
		var txErr *ddbtypes.TransactionCanceledException
		if errors.As(err, &txErr) {
			return s.getUserByEmailRecord(ctx, email)
		}
		return nil, fmt.Errorf("dynamodb: creating user: %w", err)
	}
	return user, nil
}

func (s *Store) StoreToken(ctx context.Context, token *core.IntegrationToken) error {
	accessEnc, refreshEnc, err := s.enc.EncryptTokenPair(token.AccessToken, token.RefreshToken)
	if err != nil {
		return fmt.Errorf("dynamodb: %w", err)
	}

	item := marshalIntegrationToken(token, accessEnc, refreshEnc)
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.tableName,
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("dynamodb: storing token: %w", err)
	}
	return nil
}

func (s *Store) Token(ctx context.Context, userID, integration, connection, instance string) (*core.IntegrationToken, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &s.tableName,
		Key:       tokenKey(userID, integration, connection, instance),
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb: getting token: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}
	return s.unmarshalIntegrationToken(out.Item)
}

func (s *Store) ListTokens(ctx context.Context, userID string) ([]*core.IntegrationToken, error) {
	keyCond := expression.KeyAnd(
		expression.Key(attrPK).Equal(expression.Value(userPKPrefix+userID)),
		expression.KeyBeginsWith(expression.Key(attrSK), tokenSKPrefix),
	)
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).Build()
	if err != nil {
		return nil, fmt.Errorf("dynamodb: building expression: %w", err)
	}
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &s.tableName,
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb: listing tokens: %w", err)
	}

	tokens := make([]*core.IntegrationToken, 0, len(out.Items))
	for _, item := range out.Items {
		t, err := s.unmarshalIntegrationToken(item)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

func (s *Store) ListTokensForIntegration(ctx context.Context, userID, integration string) ([]*core.IntegrationToken, error) {
	keyCond := expression.KeyAnd(
		expression.Key(attrPK).Equal(expression.Value(userPKPrefix+userID)),
		expression.KeyBeginsWith(expression.Key(attrSK), tokenSKPrefix),
	)
	filt := expression.Name(attrIntegration).Equal(expression.Value(integration))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).WithFilter(filt).Build()
	if err != nil {
		return nil, fmt.Errorf("dynamodb: building expression: %w", err)
	}
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &s.tableName,
		KeyConditionExpression:    expr.KeyCondition(),
		FilterExpression:          expr.Filter(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb: listing tokens for integration: %w", err)
	}

	tokens := make([]*core.IntegrationToken, 0, len(out.Items))
	for _, item := range out.Items {
		t, err := s.unmarshalIntegrationToken(item)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

func (s *Store) ListTokensForConnection(ctx context.Context, userID, integration, connection string) ([]*core.IntegrationToken, error) {
	keyCond := expression.KeyAnd(
		expression.Key(attrPK).Equal(expression.Value(userPKPrefix+userID)),
		expression.KeyBeginsWith(expression.Key(attrSK), tokenSKPrefix),
	)
	filt := expression.And(
		expression.Name(attrIntegration).Equal(expression.Value(integration)),
		expression.Name(attrConnection).Equal(expression.Value(connection)),
	)
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).WithFilter(filt).Build()
	if err != nil {
		return nil, fmt.Errorf("dynamodb: building expression: %w", err)
	}
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &s.tableName,
		KeyConditionExpression:    expr.KeyCondition(),
		FilterExpression:          expr.Filter(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb: listing tokens for connection: %w", err)
	}

	tokens := make([]*core.IntegrationToken, 0, len(out.Items))
	for _, item := range out.Items {
		t, err := s.unmarshalIntegrationToken(item)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

func (s *Store) DeleteToken(ctx context.Context, id string) error {
	pk, sk, err := s.lookupKeysByGSI(ctx, gsiID, attrID, id, tokenSKPrefix)
	if err != nil {
		return fmt.Errorf("dynamodb: looking up token for delete: %w", err)
	}
	if pk == "" {
		return nil
	}
	_, err = s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &s.tableName,
		Key: map[string]ddbtypes.AttributeValue{
			attrPK: &ddbtypes.AttributeValueMemberS{Value: pk},
			attrSK: &ddbtypes.AttributeValueMemberS{Value: sk},
		},
	})
	if err != nil {
		return fmt.Errorf("dynamodb: deleting token: %w", err)
	}
	return nil
}

func (s *Store) StoreAPIToken(ctx context.Context, token *core.APIToken) error {
	item := marshalAPIToken(token)
	_, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.tableName,
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("dynamodb: storing api token: %w", err)
	}
	return nil
}

func (s *Store) ValidateAPIToken(ctx context.Context, hashedToken string) (*core.APIToken, error) {
	keyCond := expression.Key(attrHashedToken).Equal(expression.Value(hashedToken))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).Build()
	if err != nil {
		return nil, fmt.Errorf("dynamodb: building expression: %w", err)
	}
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &s.tableName,
		IndexName:                 aws.String(gsiHashedToken),
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb: validating api token: %w", err)
	}
	if len(out.Items) == 0 {
		return nil, nil
	}
	token, err := unmarshalAPIToken(out.Items[0])
	if err != nil {
		return nil, err
	}
	if token.ExpiresAt != nil && time.Now().After(*token.ExpiresAt) {
		return nil, nil
	}
	return token, nil
}

func (s *Store) ListAPITokens(ctx context.Context, userID string) ([]*core.APIToken, error) {
	keyCond := expression.KeyAnd(
		expression.Key(attrPK).Equal(expression.Value(userPKPrefix+userID)),
		expression.KeyBeginsWith(expression.Key(attrSK), apiTokenSKPrefix),
	)
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).Build()
	if err != nil {
		return nil, fmt.Errorf("dynamodb: building expression: %w", err)
	}
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &s.tableName,
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb: listing api tokens: %w", err)
	}

	tokens := make([]*core.APIToken, 0, len(out.Items))
	for _, item := range out.Items {
		t, err := unmarshalAPIToken(item)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

func (s *Store) RevokeAPIToken(ctx context.Context, userID, id string) error {
	pk := userPKPrefix + userID
	sk := apiTokenSKPrefix + id

	cond := expression.Name(attrPK).AttributeExists()
	condExpr, err := expression.NewBuilder().WithCondition(cond).Build()
	if err != nil {
		return fmt.Errorf("dynamodb: building condition: %w", err)
	}

	_, err = s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &s.tableName,
		Key: map[string]ddbtypes.AttributeValue{
			attrPK: &ddbtypes.AttributeValueMemberS{Value: pk},
			attrSK: &ddbtypes.AttributeValueMemberS{Value: sk},
		},
		ConditionExpression:       condExpr.Condition(),
		ExpressionAttributeNames:  condExpr.Names(),
		ExpressionAttributeValues: condExpr.Values(),
	})
	if err != nil {
		var condErr *ddbtypes.ConditionalCheckFailedException
		if errors.As(err, &condErr) {
			return core.ErrNotFound
		}
		return fmt.Errorf("dynamodb: revoking api token: %w", err)
	}
	return nil
}

func (s *Store) RevokeAllAPITokens(ctx context.Context, userID string) (int64, error) {
	pk := userPKPrefix + userID
	keyCond := expression.KeyAnd(
		expression.Key(attrPK).Equal(expression.Value(pk)),
		expression.KeyBeginsWith(expression.Key(attrSK), apiTokenSKPrefix),
	)
	proj := expression.NamesList(expression.Name(attrPK), expression.Name(attrSK))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).WithProjection(proj).Build()
	if err != nil {
		return 0, fmt.Errorf("dynamodb: building expression: %w", err)
	}

	var items []map[string]ddbtypes.AttributeValue
	var exclusiveStartKey map[string]ddbtypes.AttributeValue
	for {
		input := &dynamodb.QueryInput{
			TableName:                 &s.tableName,
			KeyConditionExpression:    expr.KeyCondition(),
			ProjectionExpression:      expr.Projection(),
			ExpressionAttributeNames:  expr.Names(),
			ExpressionAttributeValues: expr.Values(),
			ExclusiveStartKey:         exclusiveStartKey,
		}
		out, err := s.client.Query(ctx, input)
		if err != nil {
			return 0, fmt.Errorf("dynamodb: querying api tokens for revoke-all: %w", err)
		}
		items = append(items, out.Items...)
		if out.LastEvaluatedKey == nil {
			break
		}
		exclusiveStartKey = out.LastEvaluatedKey
	}

	if len(items) == 0 {
		return 0, nil
	}

	const batchSize = 25
	const maxRetries = 3
	var count int64
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		requests := make([]ddbtypes.WriteRequest, 0, end-i)
		for _, item := range items[i:end] {
			requests = append(requests, ddbtypes.WriteRequest{
				DeleteRequest: &ddbtypes.DeleteRequest{
					Key: map[string]ddbtypes.AttributeValue{
						attrPK: item[attrPK],
						attrSK: item[attrSK],
					},
				},
			})
		}
		pending := requests
		for attempt := 0; attempt <= maxRetries && len(pending) > 0; attempt++ {
			batchOut, err := s.client.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
				RequestItems: map[string][]ddbtypes.WriteRequest{
					s.tableName: pending,
				},
			})
			if err != nil {
				return count, fmt.Errorf("dynamodb: batch deleting api tokens: %w", err)
			}
			unprocessed := batchOut.UnprocessedItems[s.tableName]
			count += int64(len(pending) - len(unprocessed))
			pending = unprocessed
		}
		if len(pending) > 0 {
			return count, fmt.Errorf("dynamodb: %d api token deletions failed after retries", len(pending))
		}
	}
	return count, nil
}

func (s *Store) lookupKeysByGSI(ctx context.Context, indexName, keyAttr, keyValue, skPrefix string) (pk, sk string, err error) {
	keyCond := expression.KeyAnd(
		expression.Key(keyAttr).Equal(expression.Value(keyValue)),
		expression.KeyBeginsWith(expression.Key(attrSK), skPrefix),
	)
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).Build()
	if err != nil {
		return "", "", fmt.Errorf("building expression: %w", err)
	}
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &s.tableName,
		IndexName:                 aws.String(indexName),
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		return "", "", err
	}
	if len(out.Items) == 0 {
		return "", "", nil
	}
	if err := attributevalue.Unmarshal(out.Items[0][attrPK], &pk); err != nil {
		return "", "", err
	}
	if err := attributevalue.Unmarshal(out.Items[0][attrSK], &sk); err != nil {
		return "", "", err
	}
	return pk, sk, nil
}

func (s *Store) getUserByEmailRecord(ctx context.Context, email string) (*core.User, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      &s.tableName,
		ConsistentRead: aws.Bool(true),
		Key: map[string]ddbtypes.AttributeValue{
			attrPK: &ddbtypes.AttributeValueMemberS{Value: emailPKPrefix + email},
			attrSK: &ddbtypes.AttributeValueMemberS{Value: uniqueEmailSK},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb: reading email uniqueness record: %w", err)
	}
	if out.Item == nil {
		return nil, fmt.Errorf("dynamodb: email uniqueness record not found after transaction conflict")
	}
	var userID string
	if err := unmarshalS(out.Item, attrID, &userID); err != nil {
		return nil, fmt.Errorf("dynamodb: unmarshaling email record user id: %w", err)
	}
	return s.GetUser(ctx, userID)
}

func (s *Store) queryUserByEmail(ctx context.Context, email string) (*core.User, error) {
	keyCond := expression.Key(attrEmail).Equal(expression.Value(email))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).Build()
	if err != nil {
		return nil, fmt.Errorf("dynamodb: building expression: %w", err)
	}
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &s.tableName,
		IndexName:                 aws.String(gsiEmail),
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb: querying user by email: %w", err)
	}
	if len(out.Items) == 0 {
		return nil, nil
	}
	return unmarshalUser(out.Items[0])
}

func userKey(id string) map[string]ddbtypes.AttributeValue {
	return map[string]ddbtypes.AttributeValue{
		attrPK: &ddbtypes.AttributeValueMemberS{Value: userPKPrefix + id},
		attrSK: &ddbtypes.AttributeValueMemberS{Value: profileSK},
	}
}

func tokenKey(userID, integration, connection, instance string) map[string]ddbtypes.AttributeValue {
	return map[string]ddbtypes.AttributeValue{
		attrPK: &ddbtypes.AttributeValueMemberS{Value: userPKPrefix + userID},
		attrSK: &ddbtypes.AttributeValueMemberS{Value: tokenSKPrefix + integration + "#" + connection + "#" + instance},
	}
}

func marshalUser(u *core.User) map[string]ddbtypes.AttributeValue {
	return map[string]ddbtypes.AttributeValue{
		attrPK:          &ddbtypes.AttributeValueMemberS{Value: userPKPrefix + u.ID},
		attrSK:          &ddbtypes.AttributeValueMemberS{Value: profileSK},
		attrID:          &ddbtypes.AttributeValueMemberS{Value: u.ID},
		attrEmail:       &ddbtypes.AttributeValueMemberS{Value: u.Email},
		attrDisplayName: &ddbtypes.AttributeValueMemberS{Value: u.DisplayName},
		attrCreatedAt:   &ddbtypes.AttributeValueMemberS{Value: u.CreatedAt.Format(time.RFC3339)},
		attrUpdatedAt:   &ddbtypes.AttributeValueMemberS{Value: u.UpdatedAt.Format(time.RFC3339)},
	}
}

func unmarshalUser(item map[string]ddbtypes.AttributeValue) (*core.User, error) {
	var u core.User
	var createdAt, updatedAt string
	if err := unmarshalS(item, attrID, &u.ID); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrEmail, &u.Email); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrDisplayName, &u.DisplayName); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrCreatedAt, &createdAt); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrUpdatedAt, &updatedAt); err != nil {
		return nil, err
	}

	var err error
	u.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	u.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing updated_at: %w", err)
	}
	return &u, nil
}

func marshalIntegrationToken(t *core.IntegrationToken, accessEnc, refreshEnc string) map[string]ddbtypes.AttributeValue {
	item := map[string]ddbtypes.AttributeValue{
		attrPK:                &ddbtypes.AttributeValueMemberS{Value: userPKPrefix + t.UserID},
		attrSK:                &ddbtypes.AttributeValueMemberS{Value: tokenSKPrefix + t.Integration + "#" + t.Connection + "#" + t.Instance},
		attrID:                &ddbtypes.AttributeValueMemberS{Value: t.ID},
		attrUserID:            &ddbtypes.AttributeValueMemberS{Value: t.UserID},
		attrIntegration:       &ddbtypes.AttributeValueMemberS{Value: t.Integration},
		attrConnection:        &ddbtypes.AttributeValueMemberS{Value: t.Connection},
		attrInstance:          &ddbtypes.AttributeValueMemberS{Value: t.Instance},
		attrAccessTokenEnc:    &ddbtypes.AttributeValueMemberS{Value: accessEnc},
		attrRefreshTokenEnc:   &ddbtypes.AttributeValueMemberS{Value: refreshEnc},
		attrScopes:            &ddbtypes.AttributeValueMemberS{Value: t.Scopes},
		attrRefreshErrorCount: &ddbtypes.AttributeValueMemberN{Value: strconv.Itoa(t.RefreshErrorCount)},
		attrMetadataJSON:      &ddbtypes.AttributeValueMemberS{Value: t.MetadataJSON},
		attrCreatedAt:         &ddbtypes.AttributeValueMemberS{Value: t.CreatedAt.Format(time.RFC3339)},
		attrUpdatedAt:         &ddbtypes.AttributeValueMemberS{Value: t.UpdatedAt.Format(time.RFC3339)},
	}
	if t.ExpiresAt != nil {
		item[attrExpiresAt] = &ddbtypes.AttributeValueMemberS{Value: t.ExpiresAt.Format(time.RFC3339)}
	}
	if t.LastRefreshedAt != nil {
		item[attrLastRefreshedAt] = &ddbtypes.AttributeValueMemberS{Value: t.LastRefreshedAt.Format(time.RFC3339)}
	}
	return item
}

func (s *Store) unmarshalIntegrationToken(item map[string]ddbtypes.AttributeValue) (*core.IntegrationToken, error) {
	var t core.IntegrationToken
	var accessEnc, refreshEnc string
	var createdAt, updatedAt, lastRefreshed, refreshCount string

	if err := unmarshalS(item, attrID, &t.ID); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrUserID, &t.UserID); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrIntegration, &t.Integration); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrConnection, &t.Connection); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrInstance, &t.Instance); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrAccessTokenEnc, &accessEnc); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrRefreshTokenEnc, &refreshEnc); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrScopes, &t.Scopes); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrMetadataJSON, &t.MetadataJSON); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrCreatedAt, &createdAt); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrUpdatedAt, &updatedAt); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrLastRefreshedAt, &lastRefreshed); err != nil {
		return nil, err
	}

	if v, ok := item[attrRefreshErrorCount]; ok {
		if err := attributevalue.Unmarshal(v, &refreshCount); err != nil {
			return nil, fmt.Errorf("unmarshaling refresh_error_count: %w", err)
		}
		t.RefreshErrorCount, _ = strconv.Atoi(refreshCount)
	}

	var err error
	t.AccessToken, t.RefreshToken, err = s.enc.DecryptTokenPair(accessEnc, refreshEnc)
	if err != nil {
		return nil, fmt.Errorf("dynamodb: %w", err)
	}
	t.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	t.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing updated_at: %w", err)
	}
	if lastRefreshed != "" {
		parsed, err := time.Parse(time.RFC3339, lastRefreshed)
		if err != nil {
			return nil, fmt.Errorf("parsing last_refreshed_at: %w", err)
		}
		t.LastRefreshedAt = &parsed
	}

	if v, ok := item[attrExpiresAt]; ok {
		var expiresStr string
		if err := attributevalue.Unmarshal(v, &expiresStr); err != nil {
			return nil, fmt.Errorf("unmarshaling expires_at: %w", err)
		}
		if expiresStr != "" {
			exp, err := time.Parse(time.RFC3339, expiresStr)
			if err != nil {
				return nil, fmt.Errorf("parsing expires_at: %w", err)
			}
			t.ExpiresAt = &exp
		}
	}

	return &t, nil
}

func marshalAPIToken(t *core.APIToken) map[string]ddbtypes.AttributeValue {
	item := map[string]ddbtypes.AttributeValue{
		attrPK:          &ddbtypes.AttributeValueMemberS{Value: userPKPrefix + t.UserID},
		attrSK:          &ddbtypes.AttributeValueMemberS{Value: apiTokenSKPrefix + t.ID},
		attrID:          &ddbtypes.AttributeValueMemberS{Value: t.ID},
		attrUserID:      &ddbtypes.AttributeValueMemberS{Value: t.UserID},
		attrName:        &ddbtypes.AttributeValueMemberS{Value: t.Name},
		attrHashedToken: &ddbtypes.AttributeValueMemberS{Value: t.HashedToken},
		attrScopes:      &ddbtypes.AttributeValueMemberS{Value: t.Scopes},
		attrCreatedAt:   &ddbtypes.AttributeValueMemberS{Value: t.CreatedAt.Format(time.RFC3339)},
		attrUpdatedAt:   &ddbtypes.AttributeValueMemberS{Value: t.UpdatedAt.Format(time.RFC3339)},
	}
	if t.ExpiresAt != nil {
		item[attrExpiresAt] = &ddbtypes.AttributeValueMemberS{Value: t.ExpiresAt.Format(time.RFC3339)}
	}
	return item
}

func unmarshalAPIToken(item map[string]ddbtypes.AttributeValue) (*core.APIToken, error) {
	var t core.APIToken
	var createdAt, updatedAt string

	if err := unmarshalS(item, attrID, &t.ID); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrUserID, &t.UserID); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrName, &t.Name); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrHashedToken, &t.HashedToken); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrScopes, &t.Scopes); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrCreatedAt, &createdAt); err != nil {
		return nil, err
	}
	if err := unmarshalS(item, attrUpdatedAt, &updatedAt); err != nil {
		return nil, err
	}

	var err error
	t.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	t.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing updated_at: %w", err)
	}

	if v, ok := item[attrExpiresAt]; ok {
		var expiresStr string
		if err := attributevalue.Unmarshal(v, &expiresStr); err != nil {
			return nil, fmt.Errorf("unmarshaling expires_at: %w", err)
		}
		if expiresStr != "" {
			exp, err := time.Parse(time.RFC3339, expiresStr)
			if err != nil {
				return nil, fmt.Errorf("parsing expires_at: %w", err)
			}
			t.ExpiresAt = &exp
		}
	}

	return &t, nil
}

func unmarshalS(item map[string]ddbtypes.AttributeValue, key string, dest *string) error {
	v, ok := item[key]
	if !ok {
		*dest = ""
		return nil
	}
	return attributevalue.Unmarshal(v, dest)
}
