# PostgreSQL 每日异地备份

## R2 配置

用户提供的地址：

```text
https://195820d9dc53137c78d42b45e78d3180.r2.cloudflarestorage.com/codego
```

拆分为：

```text
R2_ENDPOINT=https://195820d9dc53137c78d42b45e78d3180.r2.cloudflarestorage.com
R2_BUCKET=codego
BACKUP_PREFIX=codego/postgres
AWS_DEFAULT_REGION=auto
```

不要把 `/codego` 放进 `R2_ENDPOINT`，否则 AWS CLI 会把 bucket 重复拼接。

## 服务器依赖

在生产数据盘安装：

- PostgreSQL 客户端（提供 `pg_dump`）
- `aws` CLI
- `age`

生成一对只用于备份的 age 密钥，并把私钥离线保存。服务器只保存公钥：

```bash
age-keygen -o /root/.config/codego/backup-key.txt
chmod 600 /root/.config/codego/backup-key.txt
grep '^# public key:' /root/.config/codego/backup-key.txt
```

## 环境文件

创建 `/etc/codego/postgres-r2-backup.env`，权限必须为 `600`：

```dotenv
SQL_DSN=postgresql://readonly_backup_user:REDACTED@127.0.0.1:5432/neondb?sslmode=disable
R2_ENDPOINT=https://195820d9dc53137c78d42b45e78d3180.r2.cloudflarestorage.com
R2_BUCKET=codego
BACKUP_PREFIX=codego/postgres
R2_ACCESS_KEY_ID=REDACTED
R2_SECRET_ACCESS_KEY=REDACTED
AGE_RECIPIENT=age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
RETENTION_DAYS=35
BACKUP_TMP_DIR=/mnt/codego-data/postgres-backups/tmp
```

建议建立只读备份数据库用户，避免备份任务拥有写权限。R2 Token 只授予 `codego` bucket 的对象读写和删除权限，不授予账户管理权限。

## 手动测试

```bash
set -a
. /etc/codego/postgres-r2-backup.env
set +a
/opt/codego/scripts/backup-postgres-r2.sh
```

## 每日执行

使用 systemd timer，避免依赖容器内部 cron：

脚本会在临时目录创建 `.backup.lock`，同一时间只允许一个备份任务运行；重复触发会安全退出，不会覆盖正在生成的文件。

`/etc/systemd/system/codego-postgres-backup.service`：

```ini
[Unit]
Description=CodeGo encrypted PostgreSQL backup to Cloudflare R2
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
EnvironmentFile=/etc/codego/postgres-r2-backup.env
ExecStart=/opt/codego/scripts/backup-postgres-r2.sh
```

`/etc/systemd/system/codego-postgres-backup.timer`：

```ini
[Unit]
Description=Daily CodeGo PostgreSQL backup

[Timer]
OnCalendar=*-*-* 04:20:00 UTC
Persistent=true
RandomizedDelaySec=900

[Install]
WantedBy=timers.target
```

启用：

```bash
systemctl daemon-reload
systemctl enable --now codego-postgres-backup.timer
systemctl start codego-postgres-backup.service
systemctl list-timers codego-postgres-backup.timer
```

## 恢复演练

备份文件是 `age` 加密的 PostgreSQL custom dump。恢复时先在临时 PostgreSQL 实例执行：

```bash
age --decrypt -i /root/.config/codego/backup-key.txt codego-YYYYMMDDTHHMMSSZ.dump.age > codego.dump
pg_restore --list codego.dump >/dev/null
createdb codego_restore_check
pg_restore --exit-on-error --no-owner --dbname=codego_restore_check codego.dump
```

每周至少执行一次恢复演练，并检查用户、订阅、账本、盲盒订单和路由配置数量。备份成功不等于可恢复，必须保留恢复验证记录。
