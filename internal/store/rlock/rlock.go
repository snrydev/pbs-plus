package rlock

import (
	"context"
	"time"

	"github.com/bsm/redislock"
	"github.com/redis/go-redis/v9"
)

type RedisInstance struct {
	*redis.Client
	lockClient *redislock.Client
	ctx        context.Context
}

type RedisLock struct {
	*redislock.Lock
	instance *RedisInstance
}

func NewRedis(ctx context.Context) *RedisInstance {
	// Connect to redis.
	client := redis.NewClient(&redis.Options{
		Network: "tcp",
		Addr:    "127.0.0.1:6379",
	})

	locker := redislock.New(client)

	return &RedisInstance{
		ctx:        ctx,
		Client:     client,
		lockClient: locker,
	}
}

func (r *RedisInstance) Close() {
	r.Client.Close()
}

func (r *RedisInstance) Lock(key string) (*RedisLock, error) {
	lock, err := r.lockClient.Obtain(r.ctx, key, 1*time.Minute, &redislock.Options{
		RetryStrategy: redislock.ExponentialBackoff(100*time.Millisecond, 10*time.Second),
	})
	if err != nil {
		return nil, err
	}

	return &RedisLock{Lock: lock, instance: r}, nil
}

func (r *RedisInstance) TryLock(key string) (*RedisLock, error) {
	lock, err := r.lockClient.Obtain(r.ctx, key, 1*time.Minute, &redislock.Options{
		RetryStrategy: redislock.NoRetry(),
	})
	if err != nil {
		return nil, err
	}

	return &RedisLock{Lock: lock, instance: r}, nil
}

func (l *RedisLock) Refresh(ttl time.Duration) error {
	return l.Lock.Refresh(l.instance.ctx, ttl, &redislock.Options{
		RetryStrategy: redislock.ExponentialBackoff(100*time.Millisecond, 10*time.Second),
	})
}

func (l *RedisLock) RefreshUntilContext(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
				l.Refresh(1 * time.Minute)
			}
		}
	}()
}

func (l *RedisLock) Unlock() error {
	return l.Lock.Release(l.instance.ctx)
}
