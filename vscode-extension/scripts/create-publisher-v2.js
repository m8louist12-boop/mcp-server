/**
 * VS Code Marketplace Publisher 创建工具 v2
 *
 * 流积: 设备代码流(/common) -> 获取 token -> 创建 publisher
 *
 * 使用方法:
 *   node scripts/create-publisher-v2.js
 */

const https = require('https');

const CONFIG = {
  publisherName: 'Correctover',
  displayName: 'Correctover',
  description: 'MCP Reliability Layer for AI — validate, verify, and self-heal every LLM response automatically.',
  emailAddress: '234114134@qq.com',
  companyWebsite: 'https://correctover.com',
  supportUrl: 'https://github.com/Correctover/mcp-server/issues',
};

function postForm(host, path, body) {
  return new Promise((resolve, reject) => {
    const data = typeof body === 'string' ? body : new URLSearchParams(body).toString();
    const req = https.request({ hostname: host, path, method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded', 'Content-Length': Buffer.byteLength(data) } }, res => {
      let b = '';
      res.on('data', c => b += c);
      res.on('end', () => resolve({ status: res.statusCode, headers: res.headers, body: b }));
    });
    req.on('error', reject);
    req.write(data);
    req.end();
  });
}

function postJson(host, path, body, token) {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify(body);
    const headers = { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) };
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const req = https.request({ hostname: host, path, method: 'POST', headers }, res => {
      let b = '';
      res.on('data', c => b += c);
      res.on('end', () => resolve({ status: res.statusCode, body: b }));
    });
    req.on('error', reject);
    req.write(data);
    req.end();
  });
}

function wait(ms) {
  return new Promise(r => setTimeout(r, ms));
}

async function main() {
  console.log('');
  console.log('╔══════════════════════════════════════════════════════════╗');
  console.log('║   VS Code Marketplace — Publisher 创建 v2               ║');
  console.log('╚══════════════════════════════════════════════════════════╝');
  console.log('');

  // Step 1: 请求设备代码
  console.log('📋 步骤 1: 请求设备代码...');
  const dcResp = await postForm('login.microsoftonline.com', '/common/oauth2/v2.0/devicecode', {
    client_id: '04b07795-8ddb-461a-bbee-02f9e1bf7b46',
    scope: '499b84ac-1321-427f-aa17-267ca6975798/.default',
  });

  if (dcResp.status !== 200) {
    console.error('   ❌ 设备代码请求失败:', dcResp.status, dcResp.body);
    return;
  }

  const dc = JSON.parse(dcResp.body);
  console.log('   ✅ 设备代码已获取');
  console.log('');
  console.log('🔑 请在浏览器中打开以下链接：');
  console.log('');
  console.log(`   🌐 ${dc.verification_uri}`);
  console.log(`   🔢 验证码: ${dc.user_code}`);
  console.log('');
  console.log('   使用你的 Microsoft 账号登录');
  console.log('   (234114134@qq.com — 或已关联的 GitHub 账号)');
  console.log('');
  console.log('   完成后按 Enter 继续...');
  console.log('   或输入 "skip" 跳过 API 创建方式，改用网页表单');
  console.log('');

  // Wait for user to press Enter (readline)
  const readline = require('readline').createInterface({ input: process.stdin, output: process.stdout });
  const answer = await new Promise(r => readline.question('   按 Enter 继续: ', r));
  readline.close();

  if (answer.toLowerCase() === 'skip') {
    console.log('');
    console.log('   跳转到网页表单指引...');
    showManualInstructions();
    return;
  }

  // Step 2: 轮询 token
  console.log('');
  console.log('📋 步骤 2: 获取访问令牌...');

  let token = null;
  const maxAttempts = 60;
  for (let i = 0; i < maxAttempts; i++) {
    const resp = await postForm('login.microsoftonline.com', '/common/oauth2/v2.0/token', {
      grant_type: 'urn:ietf:params:oauth:grant-type:device_code',
      client_id: '04b07795-8ddb-461a-bbee-02f9e1bf7b46',
      device_code: dc.device_code,
    });

    if (resp.status === 200) {
      const tr = JSON.parse(resp.body);
      token = tr.access_token;
      console.log('   ✅ Token 获取成功!');
      console.log(`   📧 账号: ${tr.id_token_claims?.email || tr.id_token_claims?.preferred_username || '未知'}`);
      break;
    }

    const err = JSON.parse(resp.body);
    if (err.error === 'authorization_pending') {
      process.stdout.write('.');
      await wait(3000);
    } else if (err.error === 'authorization_declined') {
      console.error('   ❌ 用户拒绝授权');
      return;
    } else if (err.error === 'expired_token') {
      console.error('   ❌ 设备代码已过期，请重试');
      return;
    } else {
      console.error('   ❌ 错误:', err.error, err.error_description);
      // Try one more time with a longer wait
      await wait(5000);
    }
  }

  if (!token) {
    console.error('   ❌ 未能获取 token（超时）');
    console.log('   请手动创建 publisher:');
    showManualInstructions();
    return;
  }

  console.log('');

  // Step 3: 检查 publisher 是否已存在
  console.log('📋 步骤 3: 检查 publisher 状态...');
  const checkResp = await postJson('marketplace.visualstudio.com',
    `/_apis/gallery/publishers?publisherName=${encodeURIComponent(CONFIG.publisherName)}`,
    null, token);

  if (checkResp.status === 200) {
    console.log(`   ⚠️  Publisher "${CONFIG.publisherName}" 已存在！`);
  } else if (checkResp.status === 404) {
    console.log(`   ✅ Publisher 名称可用`);
  } else {
    console.log(`   ℹ️ 检查结果: ${checkResp.status} — 继续尝试创建`);
  }

  // Step 4: 创建 publisher
  console.log('');
  console.log('📋 步骤 4: 创建 Publisher...');

  const createResp = await postJson('marketplace.visualstudio.com',
    '/_apis/gallery/publishers?api-version=3.0-preview.1',
    {
      publisherName: CONFIG.publisherName,
      displayName: CONFIG.displayName,
      description: CONFIG.description,
      emailAddress: CONFIG.emailAddress,
      companyWebsite: CONFIG.companyWebsite,
      supportUrl: CONFIG.supportUrl,
      extensions: [],
      flags: 0,
    }, token);

  console.log(`   状态: ${createResp.status}`);
  if (createResp.status >= 200 && createResp.status < 300) {
    console.log(`   ✅ Publisher "${CONFIG.publisherName}" 创建成功！`);
    console.log(`   🔗 管理: https://marketplace.visualstudio.com/manage/publishers/${CONFIG.publisherName}`);
    console.log('');
    showPublishInstructions();
  } else {
    console.log(`   ❌ 创建失败: ${createResp.body}`);
    console.log('');
    console.log('   Token 信息:', token.substring(0, 20) + '...');
    console.log('');

    // If token was issued but API rejected, try another approach
    if (createResp.status === 401 || createResp.status === 403) {
      console.log('   ⚠️ Token 可能没有足够权限。Azure DevOps 资源可能');
      console.log('   ❌ 不支持个人 Microsoft 账号的 API 访问。');
      console.log('');
      showManualInstructions();
    } else if (createResp.status === 409) {
      console.log('   ⚠️ Publisher 已存在或有冲突。');
    } else {
      showManualInstructions();
    }
  }
}

function showManualInstructions() {
  console.log('');
  console.log('╔══════════════════════════════════════════════════════════╗');
  console.log('║   手动创建 Publisher 指引                                ║');
  console.log('╚══════════════════════════════════════════════════════════╝');
  console.log('');
  console.log('   1. 打开 https://aka.ms/vscode-create-publisher');
  console.log('   2. 用 Microsoft 账号登录 (234114134@qq.com)');
  console.log('   3. 填写以下字段（全部必填）：');
  console.log('');
  console.log('      ┌─────────────────────────────────────────────┐');
  console.log('      │ Publisher Name:    Correctover              │');
  console.log('      │ Display Name:      Correctover              │');
  console.log('      │ Description:       MCP Reliability Layer    │');
  console.log('      │                    for AI — validate,       │');
  console.log('      │                    verify, and self-heal    │');
  console.log('      │                    every LLM response.      │');
  console.log('      │                                             │');
  console.log('      │ Website:           https://correctover.com  │');
  console.log('      │ Support URL:       https://github.com/      │');
  console.log('      │                    Correctover/mcp-server/  │');
  console.log('      │                    issues                   │');
  console.log('      │                                             │');
  console.log('      │ Email:             234114134@qq.com         │');
  console.log('      │ Logo:              media/publisher-         │');
  console.log('      │                    logo-128.png             │');
  console.log('      │                                             │');
  console.log('      │ ☑ I agree to the Marketplace Publisher      │');
  console.log('      │   Terms and Conditions                      │');
  console.log('      └─────────────────────────────────────────────┘');
  console.log('');
  console.log('   4. 点击 "Create" 按钮');
  console.log('');
  console.log('   常见问题:');
  console.log('   - 按钮没反应 → 可能有隐藏的必填字段，检查每个字段');
  console.log('   - 换 Chrome 而不是 Edge');
  console.log('   - 用无痕/隐私模式');
  console.log('   - 清除 cookies 后重试');
  console.log('');
  showPublishInstructions();
}

function showPublishInstructions() {
  console.log('╔══════════════════════════════════════════════════════════╗');
  console.log('║   ✅ 下一步：发布到 Marketplace                         ║');
  console.log('╚══════════════════════════════════════════════════════════╝');
  console.log('');
  console.log('   方式一：手动上传 VSIX');
  console.log('   ─────────────────────────────────────────');
  console.log('   文件: vscode-extension/correctover-vscode-1.0.0.vsix');
  console.log('   上传: https://marketplace.visualstudio.com/manage');
  console.log('');
  console.log('   方式二：Azure DevOps PAT + vsce');
  console.log('   ─────────────────────────────────────────');
  console.log('   1. 打开 https://dev.azure.com');
  console.log('   2. 登录后创建免费组织');
  console.log('   3. 头像 → Personal access tokens');
  console.log('   4. + New Token → Scopes: Marketplace (Manage)');
  console.log('   5. 复制 PAT → 运行:');
  console.log('      cd /c/d/workspace/correctover/vscode-extension');
  console.log('      npx vsce publish --pat "你的PAT"');
  console.log('');
  console.log('   方式三：发布到 Open VSX（推荐先做这个）');
  console.log('   ─────────────────────────────────────────');
  console.log('   1. 打开 https://open-vsx.org');
  console.log('   2. 用 GitHub 账号登录 ("Correctover")');
  console.log('   3. 访问 https://open-vsx.org/user-settings/tokens');
  console.log('   4. 生成 token → 运行:');
  console.log('      npx ovsx publish correctover-vscode-1.0.0.vsix --pat "token"');
  console.log('');
  console.log('   方式四（推荐）：自动发布管道');
  console.log('   ─────────────────────────────────────────');
  console.log('   将 PAT 设为 GitHub 仓库 Secret:');
  console.log('   1. Repo → Settings → Secrets → VSCE_PAT');
  console.log('   2. 打 tag: git tag vscode-v1.0.0 && git push origin vscode-v1.0.0');
  console.log('');
}

main().catch(e => { console.error('Fatal:', e.message); process.exit(1); });
