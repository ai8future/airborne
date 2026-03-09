package redis

import (
	"context"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ai8future/chassis-go-addons/rediskit"
)

// Client wraps the Redis client with Airborne-specific operations
type Client struct {
	kit *rediskit.Client
}

// Config holds Redis connection configuration
type Config struct {
	Addr     string
	Password string
	DB       int
}

// NewClient creates a new Redis client
func NewClient(cfg Config) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	kit, err := rediskit.Open(ctx, rediskit.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	if err != nil {
		return nil, err
	}
	return &Client{kit: kit}, nil
}

// Close closes the Redis connection
func (c *Client) Close() error {
	return c.kit.Close()
}

// Ping checks if Redis is reachable
func (c *Client) Ping(ctx context.Context) error {
	return c.kit.Ping(ctx)
}

// Get retrieves a value by key
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	return c.kit.Get(ctx, key)
}

// Set stores a value with optional expiration
func (c *Client) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return c.kit.Set(ctx, key, value, expiration)
}

// SetNX sets a value only if the key does not exist (atomic set-if-not-exists)
// Returns true if the key was set, false if it already existed
func (c *Client) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	return c.kit.Raw().SetNX(ctx, key, value, expiration).Result()
}

// Del deletes keys
func (c *Client) Del(ctx context.Context, keys ...string) error {
	return c.kit.Del(ctx, keys...)
}

// Exists checks if keys exist
func (c *Client) Exists(ctx context.Context, keys ...string) (int64, error) {
	return c.kit.Raw().Exists(ctx, keys...).Result()
}

// Incr increments a counter
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	return c.kit.Raw().Incr(ctx, key).Result()
}

// IncrBy increments a counter by a specific amount
func (c *Client) IncrBy(ctx context.Context, key string, value int64) (int64, error) {
	return c.kit.Raw().IncrBy(ctx, key, value).Result()
}

// Expire sets expiration on a key
func (c *Client) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return c.kit.Raw().Expire(ctx, key, expiration).Err()
}

// Eval executes a Lua script
func (c *Client) Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error) {
	return c.kit.Raw().Eval(ctx, script, keys, args...).Result()
}

// TTL gets the remaining time to live for a key
func (c *Client) TTL(ctx context.Context, key string) (time.Duration, error) {
	return c.kit.Raw().TTL(ctx, key).Result()
}

// HGet gets a hash field
func (c *Client) HGet(ctx context.Context, key, field string) (string, error) {
	return c.kit.Raw().HGet(ctx, key, field).Result()
}

// HSet sets hash fields
func (c *Client) HSet(ctx context.Context, key string, values ...interface{}) error {
	return c.kit.Raw().HSet(ctx, key, values...).Err()
}

// HGetAll gets all hash fields
func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return c.kit.Raw().HGetAll(ctx, key).Result()
}

// HDel deletes hash fields
func (c *Client) HDel(ctx context.Context, key string, fields ...string) error {
	return c.kit.Raw().HDel(ctx, key, fields...).Err()
}

// Scan iterates over keys matching a pattern
func (c *Client) Scan(ctx context.Context, pattern string) ([]string, error) {
	var keys []string
	var cursor uint64
	for {
		var batch []string
		var err error
		batch, cursor, err = c.kit.Raw().Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		if cursor == 0 {
			break
		}
	}
	return keys, nil
}

// IsNil checks if an error is redis.Nil (key not found).
// Uses errors.Is to correctly detect redis.Nil even when wrapped by rediskit.
func IsNil(err error) bool {
	return errors.Is(err, goredis.Nil)
}
