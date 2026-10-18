---
name: ikuai-routing
description: iKuai routing — static routes, traffic shunting (domain, five-tuple, L7, load-balance, updown).
---

# Routing

## 静态路由

```bash
ikuai-cli routing static list --format json
ikuai-cli routing static get <ID> --format json
ikuai-cli routing static create --name "to_lan2" --dst-addr "10.10.10.0" --gateway "10.66.0.1" --netmask "255.255.255.0" --interface wan1 --format json
ikuai-cli routing static update <ID> --name "to_lan2_bak" --priority 10 --format json
ikuai-cli routing static toggle <ID> --enabled no --format json
ikuai-cli routing static delete <ID> --yes --format json
```

## 诊断：内核残留路由与回环

```bash
export IKUAI_CLI_WEB_USER=admin IKUAI_CLI_WEB_PASSWORD=<密码>   # 当前路由表只在网页管理接口里
ikuai-cli routing diagnose --format json
```

| kind | 含义 | 处理 |
|---|---|---|
| `stale-kernel-route`（error） | 当前路由表里有一条静态路由配置解释不了的路由。修改或删除静态路由后 iKuai 可能留下它（常见为 prio 20） | 界面和接口都删不掉，且优先于端口分流：重启路由器 |
| `static-to-lan-host`（warning） | 静态路由的下一跳是局域网主机。静态路由只看目的地址，那台主机自己经路由器访问该目的时，包会被送回它，形成回环 | 确认那台主机不会自己访问该目的；或改用排除它的端口分流 |
| `policy-loop`（warning） | 下一跳类型的端口分流没有把下一跳自己排除在源地址外 | `update <ID> --src-addr <下一跳> --src-addr-inv 1` |

## 分流规则（stream）

5 种分流，统一 CRUD 模式：

### 域名分流
```bash
ikuai-cli routing stream domain list --format json
ikuai-cli routing stream domain get <ID> --format json
ikuai-cli routing stream domain create --name "baidu" --domain "www.baidu.com,baidu.com" --interface wan2 --format json
ikuai-cli routing stream domain update <ID> --name "baidu_wan1" --interface wan1 --format json
ikuai-cli routing stream domain toggle <ID> --enabled no --format json
ikuai-cli routing stream domain delete <ID> --yes --format json
```

### 五元组分流（端口分流）

两种转发方式：`--type 0`（默认）走外网线路，必须给 `--interface`；`--type 1` 交给局域网下一跳网关，必须给 `--nexthop`，线路留空。

把代理网段交给旁路网关时，**一定要排除网关自己**，否则网关自身的直连流量会被送回网关形成回环：

```bash
ikuai-cli routing stream five-tuple create --name cf_proxy --type 1 --nexthop 10.0.0.4 \
  --src-addr 10.0.0.4 --src-addr-inv 1 --dst-addr "198.18.0.0/16,91.108.0.0/16" \
  --protocol any --priority 0 --format json
```

- `--priority 0` 最高（范围 0–63），避免多线负载、协议分流等规则先把流量送到外网。
- 列表的 `src` / `dst` 列用 `!地址` 表示反向匹配（iKuai 网页列表不显示这一点）。
- 同目的的**静态路由优先于端口分流**：新规则生效前要删除对应静态路由，再用 `routing diagnose` 确认没有内核残留。

```bash
ikuai-cli routing stream five-tuple list --format json
ikuai-cli routing stream five-tuple get <ID> --format json
ikuai-cli routing stream five-tuple create --name "web_wan2" --protocol tcp --dst-port "80,443" --interface wan2 --format json
ikuai-cli routing stream five-tuple update <ID> --dst-port "8080,8443" --format json
ikuai-cli routing stream five-tuple toggle <ID> --enabled no --format json
ikuai-cli routing stream five-tuple delete <ID> --yes --format json
```

### L7 协议分流
```bash
ikuai-cli routing stream l7 list --format json
ikuai-cli routing stream l7 get <ID> --format json
ikuai-cli routing stream l7 create --name "dns_wan2" --app-proto "DNS" --interface wan2 --format json
ikuai-cli routing stream l7 update <ID> --name "dns_wan1" --interface wan1 --format json
ikuai-cli routing stream l7 toggle <ID> --enabled no --format json
ikuai-cli routing stream l7 delete <ID> --yes --format json
```

### 负载均衡
```bash
ikuai-cli routing stream load-balance list --format json
ikuai-cli routing stream load-balance get <ID> --format json
ikuai-cli routing stream load-balance create --name "lb_wan1" --interface wan1 --mode 0 --weight 1 --isp-name all --format json
ikuai-cli routing stream load-balance update <ID> --weight 2 --format json
ikuai-cli routing stream load-balance toggle <ID> --enabled no --format json
ikuai-cli routing stream load-balance delete <ID> --yes --format json
```

### 上下行分离
```bash
ikuai-cli routing stream updown list --format json
ikuai-cli routing stream updown get <ID> --format json
ikuai-cli routing stream updown create --name "split" --upiface wan1 --downiface wan2 --format json
ikuai-cli routing stream updown update <ID> --protocol tcp --dst-port "80,443" --format json
ikuai-cli routing stream updown toggle <ID> --enabled no --format json
ikuai-cli routing stream updown delete <ID> --yes --format json
```

常用对象字段使用逗号分隔，例如 `--src-addr "192.168.1.10,192.168.1.11"`、`--dst-port "80,443"`、`--app-proto "DNS,HTTP"`。
