# Correctover VS Code Extension — 商业化发布指南

## 前置条件

1. **Microsoft 账户** — 用于登录 VS Code Marketplace
2. **Node.js 22+** — 已安装
3. **GitHub 仓库** — https://github.com/Correctover/mcp-server

---

## 第一步：注册 Publisher

打开以下链接创建 Publisher：

https://aka.ms/vscode-create-publisher

- **Publisher ID**: `Correctover`
- **Display Name**: `Correctover`
- **Description**: MCP Reliability Layer for AI

## 第二步：生成 Personal Access Token (PAT)

1. 登录 https://dev.azure.com
2. 点击右上角用户头像 → **Personal access tokens**
3. 创建新 Token：
   - **Organization**: All accessible organizations
   - **Expiration**: 1 year（或更长）
   - **Scopes**: 选择 **Marketplace (Manage)**
4. 复制生成的 Token

## 第三步：配置本地环境

```bash
# Windows CMD（用管理员权限）
setx VSCE_PAT "你的PAT"

# 验证是否识别
npx vsce verify-pat Correctover
```
看到 `Verified successfully` 就说明配置完成。

## 第四步：发布到 VS Code Marketplace

```bash
cd /c/d/workspace/correctover/vscode-extension

# 方式一：直接用 PAT 发布
npx vsce publish --no-dependencies --pat "你的PAT"

# 方式二：本地打包后手动上传
npx vsce package --no-dependencies
# 然后到 https://marketplace.visualstudio.com/manage 上传 .vsix 文件
```

## 第五步（可选）：发布到 Open VSX Registry

```bash
# 1. 注册 Open VSX 账号
#    访问 https://open-vsx.org/user-settings/tokens

# 2. 生成 Token → 设置环境变量
setx OPEN_VSX_TOKEN "你的Token"

# 3. 发布
npx ovsx publish --no-dependencies --pat "%OPEN_VSX_TOKEN%"
```

## 第六步：设置 GitHub Actions 自动发布

仓库 → Settings → Secrets and variables → Actions → Add secrets：

| Secret Name | 值 |
|-------------|-----|
| `VSCE_PAT` | VS Code Marketplace PAT |
| `OPEN_VSX_TOKEN` | Open VSX Registry Token |

之后打 tag 即可自动发布：

```bash
git tag vscode-v1.0.0
git push origin vscode-v1.0.0
```

## 发布后检查清单

- [ ] marketplace 页面正常显示：https://marketplace.visualstudio.com/items?itemName=Correctover.correctover-vscode
- [ ] 图标正常显示
- [ ] README 格式正确
- [ ] GitHub Release 自动附带了 .vsix
- [ ] `code --install-extension correctover-vscode-1.0.0.vsix` 安装成功
- [ ] 侧边栏 Correctover 图标正常显示
