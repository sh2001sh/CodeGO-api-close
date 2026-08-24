# PostgreSQL 每日 R2 加密备份

生产备份脚本：`scripts/backup-postgres-r2.sh`。它会为每个备份代次生成五个 age 加密对象：

- `main-neondb.dump.age`：CodeGo 主库，包含对象所有者和 ACL
- `main-roles.sql.age`：主库角色、角色成员关系和全局权限
- `temporal.dump.age`：Temporal 主库
- `temporal-visibility.dump.age`：Temporal Visibility 库
- `temporal-roles.sql.age`：Temporal 集群角色和全局权限

每个对象都有独立的 `.sha256` 文件。默认保留最近 35 个完整代次，清理范围仅限 `codego/postgres-v2/`。

## 服务器配置

依赖：PostgreSQL 客户端、Docker CLI、AWS CLI、`age`、`flock`。

R2 凭据使用 AWS profile `codego-r2`，文件权限必须为 `600`：

```text
/root/.aws/credentials
/root/.aws/config
/etc/codego/r2-endpoint
```

age 私钥只用于恢复，生产数据库机只保留公钥：

```text
/etc/codego/backup-age-recipient
```

私钥必须保存在服务器之外，并限制为仅管理员可读。丢失私钥后无法恢复任何 R2 加密对象。

## 环境文件

`/etc/codego/postgres-r2-backup.env`：

```dotenv
AWS_PROFILE=codego-r2
R2_BUCKET=codego
BACKUP_PREFIX=codego/postgres-v2
RETENTION_DAYS=35
BACKUP_TMP_DIR=/var/lib/codego-backup/tmp
```

环境文件权限必须为 `600`。

## systemd

服务使用 `Type=oneshot`，依赖 PostgreSQL 14 和 Docker；建议设置低 CPU/I/O 优先级，避免备份扫描影响在线请求。定时器每天执行一次，并启用 `Persistent=true` 和随机延迟。

```bash
systemctl enable --now codego-postgres-backup.timer
systemctl start codego-postgres-backup.service
systemctl status codego-postgres-backup.service
systemctl list-timers codego-postgres-backup.timer
```

## 下载和恢复验证

每个代次至少执行以下验证：

1. 从 R2 下载所有五个对象及对应 `.sha256`
2. 比较下载文件的 SHA-256
3. 使用离线保存的 age 私钥解密
4. 对三个 custom dump 执行 `pg_restore --list`
5. 在隔离 PostgreSQL 实例先恢复角色，再恢复数据库并进行业务表计数

custom dump 的基本可读性检查：

```bash
age --decrypt -i backup-key.txt main-neondb.dump.age > main-neondb.dump
pg_restore --list main-neondb.dump >/dev/null
```

备份成功不等于可恢复；下载、解密和恢复验证必须保留执行记录。
