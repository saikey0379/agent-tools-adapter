# agent-tools-cli

通用 **统一工具适配层（Tool Aggregator/Adapter）**，支持 http（OpenAPI）、mcp、llm 三种方式，通过标准协议（OpenAPI/MCP）将现有的服务能力"翻译"成命令行交互和 AI 可识别的指令。

## agent-tools-cli vs 传统 CLI 对比

| 维度 | 传统 HTTP/SDK CLI<br>（如 aws-cli, gh） | 传统脚本 CLI<br>（Bash/Python/Go） | 原生 MCP CLI/Inspector | agent-tools-cli<br>（通用适配器） |
|------|----------------------------------------|-----------------------------------|----------------------|--------------------------|
| 开发成本 | 高：需手动编写每个命令的逻辑、参数解析和错误处理 | 中：逻辑灵活但缺乏标准，容易写成"面条代码" | 低：只要有 MCP Server 即可，但通常交互简陋 | **极低**：直接挂载 OpenAPI/MCP 协议，自动生成命令 |
| 维护难度 | 高：API 变更需重新编译/发布客户端 | 高：环境依赖多，脚本版本同步困难 | 低：依赖协议定义的标准 | **极低**：Server 端更新定义（JSON/YAML），Client 自动适配 |
| 交互方式 | 硬编码参数 | 硬编码逻辑 | 侧重 AI 代理使用，对人不太友好 | **全能**：实时参数执行、自然语言执行、推荐模式 |
| AI 亲和度 | 低：需要额外编写 Prompt 来描述如何调用 | 低：逻辑隐藏在代码中，LLM 难以理解 | 高：原生为 AI 设计 | **极高**：内置 LLM 适配，支持"自然语言 → 工具调用"的闭环 |
| 工具发现 | 依赖文档或 --help，静态且固定 | 依赖文档，通常不规范 | 依赖协议自述 | **动态直观**：`-l` 和 `-d` 动态实时获取最新工具 |
| 生态融合 | 孤立，通常一个 CLI 对应一个服务 | 零散 | 仅限 MCP 协议 | **打破壁垒**：一个配置中心管理 HTTP、MCP、LLM 混合工具箱 |
| 部署成本 | 分发二进制包，维护多版本 | 需解决运行环境（Python/Go 等） | 简单，但功能单一 | **极简**：一个通用 binary + 一个 config.yaml 搞定所有工具 |

### 优势

- **协议即工具**：后端开发完接口，运维/用户立即就能在 CLI 里使用，实现"开发-交付"零延时
- **LLM 意图识别**：通过 `-t llm`，用自然语言描述需求，LLM 自动完成参数转换，无需记忆参数
- **混合编排**：同一框架内整合 HTTP（业务接口）和 MCP（智能体协议），一套配置搞定
- **自文档化**：`-d` 确保文档与 Server 端元数据强一致，永远最新

## SKILL接入
```markdown
[root@localhost ~]# cat skill.md
{SKILL CONTENT}

## 工具执行
1. **查看参数** → `agent-tools-cli list_clusters -d` 根据结果生成参数
2. **执行工具** → `agent-tools-cli list_clusters [paraments]`
```
## 一、安装
### 1.1. 安装包
#### 1.1.2.PKG包【MacOS】
```bash
./build-macos.sh
```
#### 1.1.1.RPM包【Linux】
- 构建RPM包
```bash
./build-rpm.sh 
```
- 安装（RPM 包安装后在 PATH 中）
```bash
rpm -ivh /root/rpmbuild/RPMS/x86_64/agent-tools-cli-0.0.1-1.x86_64.rpm
``` 
### 1.2. 手动编译安装
- 编译
```bash
go build -o agent-tools-cli .
```
- 初始化
```bash
mv agent-tools-cli /usr/bin/
cp /etc/agent-tools/config-example.yaml ~/.agent-tools/config.yaml
```

## 二、配置

配置文件默认路径：`~/.agent-tools/config.yaml`

```bash
# 生成配置模板
agent-tools-cli config init

# 查看当前配置（token 脱敏）
agent-tools-cli config show
```

配置文件格式（参考 `config-example.yaml`）：

```yaml
servers:
  default:
    openapi:
      url: https://your-server/openapi/api.json
      check_md5: https://your-server/openapi/api.md5   # 与 check_interval 二选一
      # check_interval: 300                             # 单位 s，缓存过期时间
      headers:
        Authorization: "Bearer <your-token>"
        X-Role-Id: "<your-role-id>"
    mcp:
      url: https://your-server/mcp/sse
      headers:
        Authorization: "Bearer <your-token>"
  mcpserverA:
    mcp:
      url: https://other-server/mcp
      headers:
        Authorization: "Bearer <other-token>"
llm:
  type: openai          # openai（默认，兼容 OpenAI 接口）或 anthropic
  base_url: https://api.openai.com
  api_key: ""           # 或设置 ANTHROPIC_API_KEY 环境变量
  model: gpt-4.1
log_file: /var/log/agent-tools-cli.log
```

## 三、工具使用

在执行任何工具调用前，先发现可用工具及其参数：

### 3.1. HTTP 工具
#### 3.1.1. 列出所有 http 工具
执行：
```bash
agent-tools-cli -l
```
结果：
```text
[root@localhost ~]# agent-tools-cli -l
default/get_resource               get Content Config
...
```
#### 3.1.2. 关键词过滤
执行：
```bash
agent-tools-cli -l scripts # agent-tools-cli -l {tool_name keyword}
```
结果：
```text
[root@localhost ~]# agent-tools-cli -l script
default/get_scripts             list Content Scripts
default/list_trashed_scripts    list Trashed Scripts
[root@localhost ~]#
```

#### 3.1.3. 查看工具详情及参数
执行：# 不指定 server，则优先匹配 default server，未匹配则按顺序递归匹配
```bash
agent-tools-cli list_trashed_scripts -d # agent-tools-cli {tool_name} -d
```
结果：
```text
[root@localhost ~]# agent-tools-cli list_trashed_scripts -d
Tool:        list_trashed_scripts
Description: list Trashed Scripts

Parameters:
  --page                       [integer]
  --page_size                  [integer]
  --search                     [string]
  --sort_by                    [string]
  --sort_order                 [string]
  --access_mode                [string]
[root@localhost ~]# 
```
执行：# 指定 server，匹配失败退出
```bash
agent-tools-cli default/list_trashed_scripts -d # agent-tools-cli {server_name}/{tool_name} -d
```

#### 3.1.4. 执行工具
执行：
```bash
agent-tools-cli list_trashed_scripts --page_size 20 --page 1
```
结果：
```text
[root@localhost ~]# agent-tools-cli list_trashed_scripts --page_size 20 --page 1
{
  "data": [
    {
      ...
    }
  ],
  "total": 3
}
[root@localhost ~]# 
```

### 3.2. MCP 工具
增加 `-t mcp` 参数

```bash
# 列出所有 mcp 工具
agent-tools-cli -t mcp -l
...
```
结果：
```text
[root@localhost ~]# agent-tools-cli -t mcp -l
default/delete_kubernetes_pod            Delete a pod
...
serverA/date_get                         获取日期时间
...
```
### 3.3. LLM 工具【依赖MCP】
- 推荐指定 Tools 执行，防止 token 受限，同时提高精度
#### 3.3.1. 推荐模式【默认】
执行：
```bash
agent-tools-cli -t llm list_clusters "帮我查一下cluster列表" # agent-tools-cli -t llm {tool_name} {natural_language}
```
结果：
```text
[root@localhost ~]# agent-tools-cli -t llm list_clusters "帮我查一下cluster列表" 
agent-tools-cli -t mcp default/list_clusters # 推荐命令
[root@localhost ~]# 
```
#### 3.3.2. 执行模式 -e / --exec
执行：
```bash
agent-tools-cli -t llm list_clusters "帮我查一下cluster列表" -e # agent-tools-cli -t llm {tool_name} {natural_language} -e
```
结果：
```text
[root@localhost ~]# agent-tools-cli -t llm list_clusters "帮我查一下cluster列表" -e
→ calling list_clusters
当前有2个集群：

1. dev（开发环境，活跃）
2. qa（测试环境，活跃）

如需查看某个集群详情或做进一步操作，请告知。
[root@localhost ~]# 
```

加 `-r` 跳过 LLM 总结，直接输出工具原始返回：
```bash
agent-tools-cli -t llm list_clusters "帮我查一下cluster列表" -e -r
```
结果：
```text
[root@localhost ~]# agent-tools-cli -t llm list_clusters "帮我查一下cluster列表" -e -r
→ calling list_clusters
{
  "data": [...],
  "total": 2
}
[root@localhost ~]# 
```

#### 3.3.3. 扩展
- 存在 token 受限及精度下降风险
执行：
```bash
agent-tools-cli -t llm "帮我查一下cluster列表"
```
结果：
```text
[root@localhost ]# agent-tools-cli -t llm "帮我查一下cluster列表"
agent-tools-cli -t mcp default/list_clusters --page 1 --page_size 20
[root@localhost ~]# agent-tools-cli -t llm "帮我查一下cluster列表" -e
→ calling list_clusters
当前共2个cluster：
- dev（开发环境）
- qa（测试环境）

如需更多详细信息，可指定某个cluster名称查询。
[root@localhost ~]# 
```
## 命令速查

```bash
# --- http 调用（默认）---

# 调用 default server 的工具
agent-tools-cli {tool_name} --param1 value1 --param2 value2

# 调用指定 server 的工具
agent-tools-cli {server_name}/{tool_name} --param1 value1

# --- mcp 调用 ---

agent-tools-cli -t mcp {tool_name} --param1 value1
agent-tools-cli -t mcp {server_name}/{tool_name} --param1 value1

# --- llm 调用 ---

# 推荐模式（输出命令，不执行，默认）
agent-tools-cli -t llm {tool_name} "用自然语言描述需求"

# 执行模式（LLM 推断参数后直接执行）
agent-tools-cli -t llm {tool_name} "用自然语言描述需求" -e

# 执行模式（跳过 LLM 总结，输出原始结果）
agent-tools-cli -t llm {tool_name} "用自然语言描述需求" -e -r
```

## server 查找规则

- 只写 `{tool_name}`：先查 default server，找不到则依次查其他 server
- 写 `{server_name}/{tool_name}`：仅查指定 server，找不到直接报错，不查其他 server
- `default` server 在调用时可省略 server 前缀

## 全局 flag

| flag | 说明 |
|------|------|
| `-c, --config <path>` | 指定配置文件路径（默认 `~/.agent-tools/config.yaml`） |
| `-t, --type http\|mcp\|llm` | 调用类型（默认 http） |
| `-l, --list [<keyword>]` | 列出工具，支持关键词过滤 |
| `-d, --describe` | 查看工具详情及参数 |
| `-e, --exec` | llm 模式：直接执行（默认为推荐模式） |
| `-r, --raw` | llm 模式：配合 `-e` 使用，跳过 LLM 总结，输出工具原始返回 |
| `--refresh` | 强制重新拉取 OpenAPI spec，清理 cache 里已下线的死工具（http 模式） |

## 缓存刷新

http 模式下工具 schema 缓存在 `~/.agent-tools/cache/<server>/tools/`，默认按 `check_md5` / `check_interval` 失效；若两者都未配置，兜底 10 分钟 TTL。

如果怀疑 cache 里有服务端已下线的死工具，手动触发刷新：

```bash
# 强制重新拉取 spec 并清理 cache 里已不存在的工具
agent-tools-cli -l --refresh
```

`--refresh` 会:
1. 跳过 cache 有效性检查，重新请求 OpenAPI spec
2. 写入当前 spec 里的所有工具
3. 删除 cache 里 spec 之外的残留 `.json`（之前的版本只加不删，导致 cache 膨胀）
