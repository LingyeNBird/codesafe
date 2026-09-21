/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// cache 包：把 TypeSafe API 的判定结果按"发往上游的请求 payload 的 SHA-256"缓存进
// 用户缓存目录的 bbolt KV（cache.db），TTL 1 小时。命中即复用，不再请求。
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

// bucket 是 bbolt 里的桶名。
var bucket = []byte("resp")

// ttl 是缓存有效期：同一请求 payload 在 1 小时内复用旧结果。
const ttl = time.Hour

// Entry 是一条缓存：API 响应的原始 answers + usage + 写入时间戳。
type Entry struct {
	Answers json.RawMessage `json:"answers"`
	Usage   json.RawMessage `json:"usage"`
	Ts      int64           `json:"ts"`
}

// db 是进程内单例 bbolt 句柄（延迟打开）。
var (
	db     *bolt.DB
	dbOnce sync.Once
	dbErr  error
)

// path 返回缓存库路径：<os.UserCacheDir>/codesafe/cache.db。
func path() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "codesafe", "cache.db"), nil
}

// open 懒加载打开 bbolt；失败只记一次（dbErr），调用方据此降级为不缓存。
func open() (*bolt.DB, error) {
	dbOnce.Do(func() {
		p, err := path()
		if err != nil {
			dbErr = err
			return
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			dbErr = err
			return
		}
		d, err := bolt.Open(p, 0o600, &bolt.Options{Timeout: 2 * time.Second})
		if err != nil {
			dbErr = err
			return
		}
		err = d.Update(func(tx *bolt.Tx) error {
			_, e := tx.CreateBucketIfNotExists(bucket)
			return e
		})
		if err != nil {
			dbErr = err
			return
		}
		db = d
	})
	return db, dbErr
}

// Key 计算请求 payload 的 SHA-256（hex）——缓存键即真实发往上游的字节哈希。
func Key(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// Get 按 key 取缓存；未命中或过期返回 (nil,false)。
func Get(key string) (*Entry, bool) {
	d, err := open()
	if err != nil {
		return nil, false
	}
	var e Entry
	ok := false
	_ = d.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucket).Get([]byte(key))
		if v == nil {
			return nil
		}
		if err := json.Unmarshal(v, &e); err != nil {
			return nil
		}
		ok = true
		return nil
	})
	if !ok || time.Now().Unix()-e.Ts > int64(ttl.Seconds()) {
		return nil, false
	}
	return &e, true
}

// Set 写入一条缓存。
func Set(key string, e Entry) {
	d, err := open()
	if err != nil {
		return
	}
	e.Ts = time.Now().Unix()
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	_ = d.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucket).Put([]byte(key), data)
	})
}
