-- KEYS: user concurrency, channel concurrency, credential concurrency,
--       channel-user concurrency, user rolling-minute requests, RPM marker.
-- ARGV: request id, now ms, lease duration ms, user/channel/credential/
--       channel-user concurrency maxima, requests per minute.
-- Uses one Redis primary, like billing. Expired leases are removed atomically.
local request = ARGV[1]
local now = tonumber(ARGV[2])
if now == 0 then
    local clock = redis.call('TIME')
    now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
end
local ttl = tonumber(ARGV[3])
for i = 1, 4 do
    local limit = tonumber(ARGV[i + 3])
    if limit > 0 then
        redis.call('ZREMRANGEBYSCORE', KEYS[i], '-inf', now)
        if not redis.call('ZSCORE', KEYS[i], request) and redis.call('ZCARD', KEYS[i]) >= limit then
            if i == 1 then return 1 end
            return 2
        end
    end
end
local rpm = tonumber(ARGV[8])
if rpm > 0 then
    redis.call('ZREMRANGEBYSCORE', KEYS[5], '-inf', now - 60000)
    if redis.call('EXISTS', KEYS[6]) == 0 then
        if redis.call('ZCARD', KEYS[5]) >= rpm then return 1 end
        redis.call('ZADD', KEYS[5], now, request)
        redis.call('PEXPIRE', KEYS[5], 60001)
    end
    redis.call('SET', KEYS[6], '1', 'PX', ttl)
end
for i = 1, 4 do
    if tonumber(ARGV[i + 3]) > 0 then
        redis.call('ZADD', KEYS[i], now + ttl, request)
        redis.call('PEXPIRE', KEYS[i], ttl + 1)
    end
end
return 0
