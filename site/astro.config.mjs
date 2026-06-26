// @ts-check
import { defineConfig } from 'astro/config';
import tailwind from '@astrojs/tailwind';

// GitHub Pages 部署:site 字段是最终域名,base 是路径前缀。
// 用户/组织站点(stringtest.top)用根路径 '/';项目站点用 '/BoltCode/'。
// 先按自定义域名 stringtest.top 配根路径,后续如改回项目站点再调 base。
export default defineConfig({
  site: 'https://stringtest.top',
  base: '/',
  integrations: [tailwind({ applyBaseStyles: true })],
});
