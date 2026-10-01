-- KEYS: user, channel, credential and channel-user concurrency ZSETs.
-- ARGV: request id. Release is idempotent, and RPM history remains intact.
for i = 1, #KEYS do
    redis.call('ZREM', KEYS[i], ARGV[1])
end
return 0
