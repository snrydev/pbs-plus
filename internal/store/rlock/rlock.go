package rlock

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/bsm/redislock"
	"github.com/redis/go-redis/v9"
)

const (
	defaultLockTTL         = 30 * time.Second
	defaultRefreshInterval = 10 * time.Second
	defaultReleaseTimeout  = 5 * time.Second
	defaultAcquireTimeout  = 10 * time.Second
)

var (
	errLockNotHeld = redislock.ErrLockNotHeld
)

type RedisInstance struct {
	*redis.Client
	lockClient *redislock.Client
}

type RedisLock struct {
	*redislock.Lock
	instance *RedisInstance
	key      string
	cancel   context.CancelFunc
}

type RedisInstanceOptions struct {
	Options *redis.Options
}

func NewRedis(ctx context.Context, opts RedisInstanceOptions) (*RedisInstance, error) {
	if opts.Options == nil {
		opts.Options = &redis.Options{
			Network: "tcp",
			Addr:    "127.0.0.1:6379",
		}
	}
	client := redis.NewClient(opts.Options)

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, err
	}

	locker := redislock.New(client)

	return &RedisInstance{
		Client:     client,
		lockClient: locker,
	}, nil
}

func (r *RedisInstance) Close() error {
	return r.Client.Close()
}

func (r *RedisInstance) Lock(ctx context.Context, key string) (*RedisLock, error) {
	return r.obtainAndMonitor(ctx, key, redislock.ExponentialBackoff(100*time.Millisecond, defaultAcquireTimeout))
}

func (r *RedisInstance) TryLock(ctx context.Context, key string) (*RedisLock, error) {
	return r.obtainAndMonitor(ctx, key, redislock.NoRetry())
}

func (r *RedisInstance) obtainAndMonitor(ctx context.Context, key string, retry redislock.RetryStrategy) (*RedisLock, error) {
	obtainCtx := ctx
	if _, ok := ctx.Deadline(); !ok && retry != redislock.NoRetry() {
		var cancel context.CancelFunc
		obtainCtx, cancel = context.WithTimeout(ctx, defaultAcquireTimeout)
		defer cancel()
	}

	lock, err := r.lockClient.Obtain(obtainCtx, key, defaultLockTTL, &redislock.Options{
		RetryStrategy: retry,
	})
	if err != nil {
		return nil, err
	}

	monitorCtx, internalCancel := context.WithCancel(context.Background())

	redisLock := &RedisLock{
		Lock:     lock,
		instance: r,
		key:      key,
		cancel:   internalCancel,
	}

	go redisLock.monitorLockLifetime(ctx, monitorCtx)

	return redisLock, nil
}

func (l *RedisLock) monitorLockLifetime(lifetimeCtx, monitorCtx context.Context) {
	ticker := time.NewTicker(defaultRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-lifetimeCtx.Done():
			releaseCtx, cancel := context.WithTimeout(context.Background(), defaultReleaseTimeout)
			err := l.Lock.Release(releaseCtx)
			cancel()
			if err != nil && !errors.Is(err, errLockNotHeld) {
				log.Printf("rlock: failed to auto-release lock for key %s on context done: %v", l.key, err)
			}
			return

		case <-monitorCtx.Done():
			return

		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(context.Background(), defaultRefreshInterval)
			err := l.Lock.Refresh(refreshCtx, defaultLockTTL, nil)
			cancel()
			if err != nil {
				if errors.Is(err, errLockNotHeld) {
					log.Printf("rlock: lock for key %s lost during refresh", l.key)
					l.cancel() // Stop monitoring internally as lock is lost
					return
				}
				log.Printf("rlock: failed to refresh lock for key %s: %v", l.key, err)
			}
		}
	}
}

func (l *RedisLock) Unlock() error {
	l.cancel()

	releaseCtx, cancel := context.WithTimeout(context.Background(), defaultReleaseTimeout)
	defer cancel()
	err := l.Lock.Release(releaseCtx)

	if errors.Is(err, errLockNotHeld) {
		return nil
	}
	return err
}
