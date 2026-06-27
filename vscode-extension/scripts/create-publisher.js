/**
 * 直接通过 Microsoft 设备代码流创建 VS Code Marketplace Publisher
 *
 * 用法: node scripts/create-publisher.js
 */

const { PublicClientApplication } = require('@azure/msal-node');
const https = require('https');

const PUBLISHER_NAME = 'Correctover';
const PUBLISHER_DISPLAY_NAME = 'Correctover';
const PUBLISHER_DESC = 'MCP Reliability Layer for AI — validate, verify, and self-heal every LLM response automatically. Built-in 6-dimension validation with intelligent auto-failover across 9 LLM providers.';
const COMPANY_WEBSITE = 'https://correctover.com';
const SUPPORT_URL = 'https://github.com/Correctover/mcp-server/issues';
const EMAIL = '234114134@qq.com';

function httpFetch(url, opts = {}) {
  return new Promise((resolve, reject) => {
    const u = new URL(url);
    const mod = u.protocol === 'https:' ? https : null;
    if (!mod) return reject(new Error('Only https supported'));
    const req = mod.request(u, {
      method: opts.method || 'GET',
      headers: { 'Content-Type': 'application/json', ...opts.headers },
    }, (res) => {
      let data = '';
      res.on('data', c => data += c);
      res.on('end', () => {
        if (res.statusCode >= 200 && res.statusCode < 300) {
          try { resolve(data && JSON.parse(data)); } catch { resolve(data); }
        } else {
          reject(new Error(`HTTP ${res.statusCode} ${res.statusMessage}: ${data.slice(0,500)}`));
        }
      });
    });
    req.on('error', reject);
    if (opts.body) req.write(typeof opts.body === 'string' ? opts.body : JSON.stringify(opts.body));
    req.end();
  });
}

async function main() {
  console.log('');
  console.log('╔══════════════════════════════════════════════════════════╗');
  console.log('║   VS Code Marketplace — Publisher 创建                  ║');
  console.log('║   Correctover MCP Reliability Layer                     ║');
  console.log('╚══════════════════════════════════════════════════════════╝');
  console.log('');

  // Step 1: 设备代码流认证
  console.log('📋 步骤 1/4: 设备代码流认证...');
  console.log('');

  const pca = new PublicClientApplication({
    auth: {
      clientId: '872cd9fa-d31f-45e0-900e-4f3e643da2e0',
      authority: 'https://login.microsoftonline.com/consumers',
    }
  });

  let accessToken;
  try {
    const result = await pca.acquireTokenByDeviceCode({
      deviceCodeCallback: (response) => {
        console.log('🔑 请在浏览器中打开以下链接：');
        console.log('');
        console.log(`   🌐 ${response.verificationUri}`);
        console.log(`   🔢 验证码: ${response.userCode}`);
        console.log('');
        console.log('   使用你的 Microsoft 账号 234114134@qq.com 登录');
        console.log('   （或选择 GitHub 账号如果已关联）');
        console.log('');
        console.log('   正在等待认证... (最长5分钟)');
      },
      scopes: ['499b84ac-1321-427f-aa17-267ca6975798/.default'],
    });
    accessToken = result.accessToken;
    console.log('');
    console.log('   ✅ 认证成功！');
    console.log('');
  } catch (err) {
    console.error('   ❌ 认证失败:', err.message);
    if (err.message.includes('user_cancelled')) {
      console.log('   用户取消。');
    } else if (err.message.includes('code_expired')) {
      console.log('   验证码已过期，请重试。');
    } else {
      console.log('');
      console.log('   ⚡ 请手动创建 publisher:');
      console.log('   1. 打开 https://aka.ms/vscode-create-publisher');
      console.log('   2. 用 Microsoft 账号登录');
      console.log('   3. 填写:');
      console.log('      - Publisher Name: Correctover');
      console.log('      - Description: 上面已提供');
      console.log('      - Website: https://correctover.com');
      console.log('      - Support URL: https://github.com/Correctover/mcp-server/issues');
      console.log('      - Email: 234114134@qq.com');
      console.log('      - 上传 vscode-extension/media/publisher-logo-128.png');
      console.log('   4. 勾选 "I agree to the Marketplace Publisher Agreement"');
      console.log('   5. 点击 Create');
    }
    return null;
  }

  // Step 2: 检查 publisher 是否已存在
  console.log('📋 步骤 2/4: 检查 publisher 是否可用...');
  try {
    await httpFetch(
      `https://marketplace.visualstudio.com/_apis/gallery/publishers?publisherName=${encodeURIComponent(PUBLISHER_NAME)}`,
      { headers: { Authorization: `Bearer ${accessToken}`, Accept: 'application/json;api-version=3.0-preview.1' } }
    );
    console.log(`   ⚠️  Publisher "${PUBLISHER_NAME}" 已存在！`);
    console.log(`   🔗 https://marketplace.visualstudio.com/manage/publishers/${PUBLISHER_NAME}`);
    console.log('');
    return accessToken;
  } catch (e) {
    if (e.message.includes('404')) {
      console.log('   ✅ Publisher 名称可用，继续创建...');
    } else {
      console.log(`   ℹ️  需要创建 publisher: ${e.message}`);
    }
  }

  // Step 3: 创建 publisher
  console.log('');
  console.log('📋 步骤 3/4: 创建 Publisher...');
  try {
    const result = await httpFetch(
      `https://marketplace.visualstudio.com/_apis/gallery/publishers?api-version=3.0-preview.1`,
      {
        method: 'POST',
        headers: { Authorization: `Bearer ${accessToken}`, 'Content-Type': 'application/json' },
        body: {
          publisherName: PUBLISHER_NAME,
          displayName: PUBLISHER_DISPLAY_NAME,
          description: PUBLISHER_DESC,
          emailAddress: EMAIL,
          companyWebsite: COMPANY_WEBSITE,
          supportUrl: SUPPORT_URL,
          extensions: [],
          flags: 0,
        },
      }
    );
    console.log(`   ✅ Publisher "${PUBLISHER_NAME}" 创建成功！`);
    console.log(`   🆔  ID: ${result.publisherId || result.publisherName}`);
    console.log(`   🔗 管理: https://marketplace.visualstudio.com/manage/publishers/${PUBLISHER_NAME}`);
  } catch (e) {
    console.error(`   ❌ 创建失败: ${e.message}`);
  }

  return accessToken;
}

async function showNextSteps(token) {
  console.log('');
  console.log('╔══════════════════════════════════════════════════════════╗');
  console.log('║   下一步：生成 PAT 并发布                                ║');
  console.log('╚══════════════════════════════════════════════════════════╝');
  console.log('');
  console.log('   方式一：从 Azure DevOps 生成 PAT');
  console.log('   ─────────────────────────────────────────');
  console.log('   1. 打开 https://dev.azure.com');
  console.log('   2. 用同一 Microsoft 账号登录');
  console.log('   3. 创建一个免费组织（不需要任何付费）');
  console.log('   4. 右上角头像 → Personal access tokens');
  console.log('   5. + New Token → Scopes: Marketplace (Manage)');
  console.log('   6. 复制生成的 PAT');
  console.log('   7. 运行发布命令：');
  console.log('');
  console.log('      cd /c/d/workspace/correctover/vscode-extension');
  console.log('      npx vsce publish --pat "你的PAT"');
  console.log('');
  console.log('   方式二：手动上传 VSIX');
  console.log('   ─────────────────────────────────────────');
  console.log('   到 https://marketplace.visualstudio.com/manage');
  console.log('   点击 "..." → Update → 选择 .vsix 文件');
  console.log('');
  console.log('   方式三：设置 GitHub Actions 自动发布');
  console.log('   ─────────────────────────────────────────');
  console.log('   把 PAT 加入仓库 Secrets:');
  console.log('   Repo → Settings → Secrets → VSCE_PAT');
  console.log('   然后打 tag 自动发布:');
  console.log('   git tag vscode-v1.0.0 && git push origin vscode-v1.0.0');
  console.log('');
}

main().then(token => {
  if (token) showNextSteps(token);
}).catch(e => {
  console.error('Fatal:', e.message);
  process.exit(1);
});
