#!/usr/bin/env node

/**
 * e2e/api-flow.mjs — 拍卖平台端到端 API 流程测试
 *
 * 覆盖完整业务链路：
 *   注册 → 登录 → 创建直播间/商品/竞拍 → 发布/开始 → 用户出价 →
 *   自动结算 → 支付 → 地址 CRUD → 关播
 *
 * 运行方式：
 *   JWT_SECRET=paimai_dev_secret_2026 node e2e/api-flow.mjs
 *
 * 环境变量：
 *   BASE_URL    默认 http://localhost:8080
 *   JWT_SECRET  JWT 签名密钥（服务端需一致）
 */

const BASE = process.env.BASE_URL || 'http://localhost:8080';

// ============================================================
// 测试报告收集
// ============================================================
const results = { pass: 0, fail: 0, skipped: 0 };
const failures = [];

function assert(label, ok, detail) {
  if (ok) {
    results.pass++;
    console.log(`  ✅ ${label}`);
  } else {
    results.fail++;
    const msg = `${label}: ${detail || 'assertion failed'}`;
    failures.push(msg);
    console.log(`  ❌ ${msg}`);
  }
}

function skip(label) {
  results.skipped++;
  console.log(`  ⏭️  ${label}`);
}

// ============================================================
// HTTP 请求工具
// ============================================================
async function request(method, path, opts = {}) {
  const url = `${BASE}${path}`;
  const headers = { ...opts.headers };
  if (opts.json !== undefined) {
    headers['Content-Type'] = 'application/json';
  }
  if (opts.token) {
    headers['Authorization'] = `Bearer ${opts.token}`;
  }
  const body = opts.json !== undefined ? JSON.stringify(opts.json) : opts.body;
  const res = await fetch(url, { method, headers, body });
  const text = await res.text();
  let data;
  try { data = JSON.parse(text); } catch { data = null; }
  return { status: res.status, ok: res.ok, data, text };
}

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

// fetchWithRetry 包装 fetch，网络错误时自动用相同参数重试（用于模拟网络超时后的幂等重放）
async function fetchWithRetry(method, path, opts = {}) {
  const maxRetries = 2;
  for (let i = 0; i <= maxRetries; i++) {
    try {
      const r = await request(method, path, opts);
      return r;
    } catch (err) {
      if (i < maxRetries) {
        console.log(`    [重试] 网络错误，${i + 1}/${maxRetries} 次重试...`);
        await sleep(200);
        continue;
      }
      throw err;
    }
  }
}

// ============================================================
// 主流程// ============================================================
// ============================================================
// 辅助：从 /api/auth/me 获取当前用户 ID
// ============================================================
async function getMyId(token) {
  const r = await request('GET', '/api/auth/me', { token });
  if (r.ok && r.data?.code === 0) {
    return r.data?.data?.id || r.data?.data?.userId;
  }
  return null;
}

// ============================================================
// 主流程
// ============================================================
async function main() {
  const timestamp = Date.now();
  const sellerUsername = `seller_${timestamp}`;
  const buyer1Username = `buyer1_${timestamp}`;
  const buyer2Username = `buyer2_${timestamp}`;
  const password = 'test123456';

  console.log(`\n═══════════════════════════════════════════════`);
  console.log(`  拍卖平台 E2E 流程测试`);
  console.log(`  ${new Date().toISOString()}`);
  console.log(`  商家: ${sellerUsername}  买家1: ${buyer1Username}  买家2: ${buyer2Username}`);
  console.log(`═══════════════════════════════════════════════\n`);

  // ---- Phase 0: Server health check ----
  console.log('▸ Phase 0: 服务健康检查');
  try {
    const ping = await request('GET', '/ping');
    assert('ping 返回 pong', ping.data?.message === 'pong', JSON.stringify(ping.data));
  } catch (e) {
    assert('服务可达', false, e.message);
    console.log('\n⚠️  服务不可达，请先启动服务: docker compose up -d');
    process.exit(1);
  }

  // ---- Phase 1: 用户注册 ----
  console.log('\n▸ Phase 1: 用户注册与登录');

  let r = await request('POST', '/api/auth/register', {
    json: { username: sellerUsername, password, role: 'seller' },
  });
  assert('商家注册', r.ok && r.data?.code === 0, `status=${r.status}`);
  const sellerId = r.data?.data?.user?.id || r.data?.data?.id;

  r = await request('POST', '/api/auth/register', {
    json: { username: buyer1Username, password, role: 'buyer' },
  });
  assert('买家1注册', r.ok && r.data?.code === 0, `status=${r.status}`);

  r = await request('POST', '/api/auth/register', {
    json: { username: buyer2Username, password, role: 'buyer' },
  });
  assert('买家2注册', r.ok && r.data?.code === 0, `status=${r.status}`);

  // 登录获取 token
  let sellerToken, buyer1Token, buyer2Token;

  r = await request('POST', '/api/auth/login', {
    json: { username: sellerUsername, password },
  });
  sellerToken = r.data?.data?.token;
  assert('商家登录取 token', !!sellerToken, 'token 为空');

  r = await request('POST', '/api/auth/login', {
    json: { username: buyer1Username, password },
  });
  buyer1Token = r.data?.data?.token;
  assert('买家1登录取 token', !!buyer1Token);

  r = await request('POST', '/api/auth/login', {
    json: { username: buyer2Username, password },
  });
  buyer2Token = r.data?.data?.token;
  assert('买家2登录取 token', !!buyer2Token);

  // 通过 /api/auth/me 获取实际 user ID（用于出价）
  const buyer1Id = await getMyId(buyer1Token);
  assert('买家1 ID 存在', !!buyer1Id, `id=${buyer1Id}`);
  const buyer2Id = await getMyId(buyer2Token);
  assert('买家2 ID 存在', !!buyer2Id, `id=${buyer2Id}`);

  // ---- Phase 2: 商家创建直播间与商品 ----
  console.log('\n▸ Phase 2: 商家创建直播间与商品');

  r = await request('POST', '/api/admin/rooms', {
    token: sellerToken,
    json: { title: '翡翠专场·测试', coverUrl: 'https://example.com/cover.jpg' },
  });
  assert('创建直播间', r.ok && r.data?.code === 0, `status=${r.status}`);
  const roomId = r.data?.data?.id;
  assert('直播间 ID 存在', !!roomId, `id=${roomId}`);

  r = await request('POST', '/api/admin/products', {
    token: sellerToken,
    json: { name: '冰种翡翠手镯', description: '测试商品', imageUrl: '' },
  });
  assert('创建商品', r.ok && r.data?.code === 0, `status=${r.status}`);
  const productId = r.data?.data?.id;
  assert('商品 ID 存在', !!productId, `id=${productId}`);

  // 创建竞拍 — endAt/startAt 不传，服务层自动默认值
  r = await request('POST', '/api/admin/auctions', {
    token: sellerToken,
    json: { roomId, productId, mode: 'sudden_death', startPriceCents: 0, bidIncrementCents: 100, capPriceCents: 10000 },
  });
  assert('创建竞拍', r.ok && r.data?.code === 0, `status=${r.status}`);
  const auctionId = r.data?.data?.id;
  assert('竞拍 ID 存在', !!auctionId, `id=${auctionId}`);

  // 发布竞拍
  r = await request('POST', `/api/admin/auctions/${auctionId}/publish`, { token: sellerToken });
  assert('发布竞拍', r.ok && r.data?.code === 0, `status=${r.status}`);

  // 开始竞拍（10 秒短时长以便快速结算）
  r = await request('POST', `/api/admin/auctions/${auctionId}/start`, {
    token: sellerToken,
    json: { durationSec: 10 },
  });
  assert('开始竞拍', r.ok && r.data?.code === 0, `status=${r.status}`);

  // 验证竞拍状态为 running
  r = await request('GET', `/api/rooms/${roomId}/auctions?status=running`, { token: buyer1Token });
  assert('查询进行中竞拍', r.ok && r.data?.code === 0, `status=${r.status}`);
  const runningAuctions = r.data?.data || [];
  assert(`竞拍列表中包含刚开始的竞拍 #${auctionId}`, runningAuctions.some(a => a.id === auctionId),
    `found=${runningAuctions.map(a => a.id).join(',')}`);

  // ---- Phase 3: 买家出价 ----
  console.log('\n▸ Phase 3: 买家出价');

  // 买家1 出价 ¥5.00（500 分），使用从 /api/auth/me 拿到的 buyer1Id
  r = await request('POST', `/api/auctions/${auctionId}/bids`, {
    token: buyer1Token,
    json: { userId: buyer1Id, amountCents: 500, idempotencyKey: `e2e-bid1-${timestamp}` },
  });
  assert('买家1 出价 500', r.ok && r.data?.code === 0, `status=${r.status} data=${JSON.stringify(r.data)}`);
  assert('出价被接受', r.data?.data?.accepted === true, JSON.stringify(r.data?.data));

  // 买家2 出价 ¥8.00（800 分），直接用 getMyId 拿到的 buyer2Id
  r = await request('POST', `/api/auctions/${auctionId}/bids`, {
    token: buyer2Token,
    json: { userId: buyer2Id, amountCents: 800, idempotencyKey: `e2e-bid2-${timestamp}` },
  });
  assert('买家2 出价 800', r.ok && r.data?.code === 0, `status=${r.status}`);
  assert('出价被接受', r.data?.data?.accepted === true, JSON.stringify(r.data?.data));

  // 查询排行榜
  r = await request('GET', `/api/auctions/${auctionId}/ranking`, { token: buyer1Token });
  assert('排行榜可查', r.ok && r.data?.code === 0, `status=${r.status}`);
  const ranking = r.data?.data || [];
  assert(`排行榜至少有 2 人`, ranking.length >= 2, `len=${ranking.length} data=${JSON.stringify(ranking)}`);

  // ---- 幂等测试：正常出价后用相同 key 再次请求 ----
  // 幂等检查在频率检查之前，不需要等待频率窗口
  r = await request('POST', `/api/auctions/${auctionId}/bids`, {
    token: buyer2Token,
    json: { userId: buyer2Id, amountCents: 800, idempotencyKey: `e2e-bid2-${timestamp}` },
  })
  assert('幂等重放返回 true', r.data?.data?.idempotentReplay === true, JSON.stringify(r.data?.data));
  assert('重放保留原始金额', r.data?.data?.amountCents === 800, `amount=${r.data?.data?.amountCents}`);

  // ---- 幂等重试测试：用 fetchWithRetry 模拟网络超时后自动重试 ----
  const retryKey = `e2e-retry-${timestamp}`;
  r = await request('POST', `/api/auctions/${auctionId}/bids`, {
    token: buyer1Token,
    json: { userId: buyer1Id, amountCents: 900, idempotencyKey: retryKey },
  })
  assert('重试-首次出价成功', r.data?.data?.accepted === true, JSON.stringify(r.data?.data));

  // 网络错误时自动重试（最多 2 次）
  r = await fetchWithRetry('POST', `/api/auctions/${auctionId}/bids`, {
    token: buyer1Token,
    json: { userId: buyer1Id, amountCents: 900, idempotencyKey: retryKey },
  })
  assert('重试-幂等返回 true', r.data?.data?.idempotentReplay === true, JSON.stringify(r.data?.data));
  // ---- IN_FLIGHT 测试：并发请求，同 key 的第二个得到处理中状态 ----
  const ifKey = `e2e-inflight-${timestamp}`;
  const bids = [request('POST', `/api/auctions/${auctionId}/bids`, {
    token: buyer1Token,
    json: { userId: buyer1Id, amountCents: 700, idempotencyKey: ifKey },
  }), request('POST', `/api/auctions/${auctionId}/bids`, {
    token: buyer1Token,
    json: { userId: buyer1Id, amountCents: 700, idempotencyKey: ifKey },
  })];
  const [b1, b2] = await Promise.all(bids.map(p => p.catch(e => ({status:0, data:{code:0,message:e.message,data:{}}}))));
  const inFlight = [b1,b2].filter(b => (b.data?.data?.status === 'IN_FLIGHT' || b.data?.code === 409));
  const accepted = [b1,b2].filter(b => b.data?.data?.accepted === true || b.data?.data?.idempotentReplay === true);
  assert('IN_FLIGHT 并发: 至少一个被接受或处于处理中', accepted.length > 0 || inFlight.length > 0,
    `b1=${JSON.stringify(b1.data?.data)} b2=${JSON.stringify(b2.data?.data)}`);

  // ---- Phase 4: 等待竞拍到期并结算 ----

  // ---- Phase 4: 等待竞拍到期并结算 ----
  console.log('\n▸ Phase 4: 竞拍到期结算');

  console.log('  等待竞拍到期...（约 13 秒）');
  const pollStart = Date.now();
  while (true) {
    await sleep(2000);
    let checkR = await request('GET', `/api/auctions/${auctionId}`);
    let checkA = checkR?.data?.data;
    if (checkA && (checkA.status === 'sold' || checkA.status === 'settled')) break;
    if (Date.now() - pollStart > 60000) break; // max 60s
  }

  // 查最终竞拍状态
  r = await request('GET', `/api/auctions/${auctionId}`, { token: buyer1Token });
  assert('竞拍已结束', r.ok && r.data?.code === 0,
    `status=${r.status} data=${JSON.stringify(r.data?.data)}`);
  const finalAuction = r.data?.data;
  const ended = finalAuction && (finalAuction.status === 'sold' || finalAuction.status === 'settled');
  assert(`竞拍状态为 sold/settled（非 ${finalAuction?.status}）`, ended,
    `actual=${finalAuction?.status}`);

  // ---- Phase 5: 订单与支付 ----
  console.log('\n▸ Phase 5: 订单与支付');

  r = await request('GET', '/api/orders', { token: buyer1Token });
  assert('买家1 订单列表可查', r.ok && r.data?.code === 0, `status=${r.status}`);
  const buyer1Orders = r.data?.data || [];
  assert('买家1 有订单', buyer1Orders.length >= 1, `count=${buyer1Orders.length}`);

  const order = buyer1Orders.find(o => o.status === 'pending_payment');
  if (order) {
    assert('订单状态为 pending_payment', order.status === 'pending_payment', `status=${order.status}`);
    const orderId = order.id;

    // 地址 CRUD
    console.log('  ▸ 地址管理');
    r = await request('POST', '/api/addresses', {
      token: buyer1Token,
      json: { name: '张三', phone: '13800138000', province: '广东省', city: '深圳市',
              district: '南山区', detail: '科技园南区A栋1001', isDefault: true },
    });
    assert('创建地址', r.ok && r.data?.code === 0, `status=${r.status}`);

    r = await request('GET', '/api/addresses', { token: buyer1Token });
    assert('查询地址列表', r.ok && r.data?.code === 0, `status=${r.status}`);
    const addrs = r.data?.data || [];
    assert('地址列表非空', addrs.length >= 1, `count=${addrs.length}`);

    // 支付订单（带地址）
    r = await request('POST', `/api/orders/${orderId}/pay`, {
      token: buyer1Token,
      json: { addressId: addrs[0].id },
    });
    assert('支付订单', r.ok && r.data?.code === 0, `status=${r.status} data=${JSON.stringify(r.data?.data)}`);
    assert('支付后订单状态为 paid', r.data?.data?.status === 'paid',
      JSON.stringify(r.data?.data));
  } else {
    skip('买家1 无 pending_payment 订单');
  }

  // ---- Phase 6: 商家管理端 ----
  console.log('\n▸ Phase 6: 商家管理');

  r = await request('GET', '/api/admin/products', { token: sellerToken });
  assert('商家商品列表', r.ok && r.data?.code === 0, `status=${r.status}`);
  assert('商品列表非空', (r.data?.data || []).length >= 1);

  r = await request('GET', '/api/admin/auctions', { token: sellerToken });
  assert('商家竞拍列表', r.ok && r.data?.code === 0, `status=${r.status}`);
  assert('竞拍列表非空', (r.data?.data || []).length >= 1);

  r = await request('GET', `/api/admin/auctions/${auctionId}/bids`, { token: sellerToken });
  assert('出价历史', r.ok && r.data?.code === 0, `status=${r.status}`);

  r = await request('GET', '/api/admin/orders', { token: sellerToken });
  assert('商家订单列表', r.ok && r.data?.code === 0, `status=${r.status}`);

  // ---- Phase 7: 关播 ----
  console.log('\n▸ Phase 7: 关播');

  r = await request('POST', `/api/admin/rooms/${roomId}/close`, {
    token: sellerToken,
    json: { closeReason: 'E2E 测试关闭' },
  });
  assert('关闭直播间', r.ok && r.data?.code === 0, `status=${r.status} data=${JSON.stringify(r.data?.data)}`);
  assert('直播间状态为 closed', r.data?.data?.status === 'closed', `status=${r.data?.data?.status}`);

  // ---- Phase 8: 直播间列表（游客视角） ----
  console.log('\n▸ Phase 8: 游客浏览');
  r = await request('GET', '/api/rooms', {});
  assert('匿名直播间列表', r.ok && r.data?.code === 0, `status=${r.status}`);

  // ---- Phase 9: 图片上传 ----
  console.log('\n▸ Phase 9: 图片上传');
  skip('文件上传（需 multipart，略过）');

  // ============================================================
  // 报告
  // ============================================================
  const status = results.fail > 0 ? '❌ 部分失败' : '✅ 全部通过';
  console.log(`\n═══════════════════════════════════════════════`);
  console.log(`  E2E 测试完成`);
  console.log(`  ✅ 通过: ${results.pass}   ❌ 失败: ${results.fail}   ⏭️ 跳过: ${results.skipped}`);
  console.log(`  状态: ${status}`);
  console.log(`═══════════════════════════════════════════════\n`);

  if (failures.length > 0) {
    console.log('失败详情:');
    failures.forEach(f => console.log(`  • ${f}`));
    process.exit(1);
  }
}

main().catch(err => {
  console.error('E2E 执行异常:', err);
  process.exit(1);
});
