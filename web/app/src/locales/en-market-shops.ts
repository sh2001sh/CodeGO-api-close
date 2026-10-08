const messages: Record<string, string> = {
  分组: 'Groups',
  店铺: 'Shops',
  渠道主店铺: 'Provider shops',
  全部店铺: 'All shops',
  '店铺 ID': 'Shop ID',
  进入店铺: 'Visit shop',
  查看分组: 'View group',
  找到适合的渠道主: 'Find a provider',
  '店铺展示渠道主的公开服务；实际价格与质量以具体分组为准。':
    'Browse a provider’s public services. Prices and quality are specific to each group.',
  '按分组查看模型报价和服务质量。店铺名称不代表平台认证。':
    'Compare model prices and service quality by group. A shop name does not imply platform certification.',
  搜索店铺或模型: 'Search shops or models',
  个公开分组: 'public groups',
  这家店铺暂无符合条件的公开分组: 'This shop has no public groups matching your filters',
  没有匹配的店铺: 'No matching shops',
  暂无公开店铺: 'No public shops yet',
  店铺资料: 'Shop profile',
  '店铺名称和简介审核通过后展示；审核期间保留原有公开资料。':
    'Shop name and description changes appear after review. Your current public profile remains visible while awaiting review.',
  当前公开名称: 'Current public name',
  审核状态: 'Review status',
  提交店铺资料审核: 'Submit profile for review',
  店铺名称: 'Shop name',
  '使用清晰的服务名称，不得包含广告、联系方式、外链、冒充或违规内容。':
    'Use a clear service name. Advertising, contact details, links, impersonation and prohibited content are not allowed.',
  店铺简介: 'Shop description',
  '名称可留空以恢复默认名称；自定义名称为 2–40 字，简介最多 200 字。':
    'Leave the name empty to restore the default. Custom names must have 2–40 characters; descriptions allow up to 200.',
  提交渠道后可设置店铺资料: 'Submit a channel to set up your shop profile',
  店铺资料已提交审核: 'Shop profile submitted for review',
  店铺资料审核: 'Shop profile reviews',
  恢复系统名称: 'Restore the system name',
  渠道店铺: 'Provider shop',
  审核店铺资料: 'Review shop profile',
  暂无待审核店铺: 'No shop profiles awaiting review',
  用户评价: 'User ratings',
  暂无评分: 'No ratings yet',
  位评价用户: 'rating users',
  '每位用户对每个分组保留一条评分；再次提交会更新原评分。':
    'Each user has one rating per group. Submitting again updates your existing rating.',
  登录后可评价服务: 'Sign in to rate this service',
  我的评分: 'My rating',
  更新评分: 'Update rating',
  提交评分: 'Submit rating',
  渠道主不能评价自己的服务: 'Providers cannot rate their own services',
  '真实使用此分组后可以评价，服务失败的调用也可符合资格。':
    'Use this group before rating. Requests that fail because of the service can also qualify.',
  '暂不符合评分条件，请真实使用此分组后再评价。':
    'You are not eligible to rate yet. Use this group before submitting a rating.',
  '评分已保存，社区展示将同步更新。': 'Rating saved. The community display will be synchronized.',
  '推荐：可靠性优先': 'Recommended: reliability first',
  '优先展示有足够近期请求样本的分组，再按成功率置信下界排序；同分比较所选模型报价。':
    'Groups with sufficient recent requests appear first, ordered by the lower confidence bound of their success rate. Comparable model prices break ties.',
}
export default messages
