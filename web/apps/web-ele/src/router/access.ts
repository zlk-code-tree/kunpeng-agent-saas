import type {
  ComponentRecordType,
  GenerateMenuAndRoutesOptions,
} from '@vben/types';

import { generateAccessible } from '@vben/access';
import { preferences } from '@vben/preferences';

import { ElMessage } from 'element-plus';

import { getAllMenusApi, listMenuSettingsApi } from '#/api';
import { BasicLayout, IFrameView } from '#/layouts';
import { $t } from '#/locales';

const forbiddenComponent = () => import('#/views/_core/fallback/forbidden.vue');

async function generateAccess(options: GenerateMenuAndRoutesOptions) {
  const pageMap: ComponentRecordType = import.meta.glob('../views/**/*.vue');

  const layoutMap: ComponentRecordType = {
    BasicLayout,
    IFrameView,
  };

  // 菜单配置只调整前端已经注册的路由；服务端接口仍独立校验权限。
  // 未建表或升级期间保留已编译路由，避免菜单接口故障使整个后台无法登录。
  let configuredRoutes = options.routes;
  try {
    const settings = await listMenuSettingsApi();
    const byName = new Map(settings.map((item) => [item.routeName, item]));
    configuredRoutes = options.routes
      .filter((route) => {
        const setting = byName.get(String(route.name));
        return !setting || setting.enabled || setting.protected;
      })
      .map((route) => {
        const setting = byName.get(String(route.name));
        if (!setting) return route;
        return {
          ...route,
          meta: {
            ...route.meta,
            order: setting.sortOrder,
            title: setting.title,
          },
        };
      });
  } catch (error) {
    console.warn('菜单配置暂不可用，使用内置菜单', error);
  }

  return await generateAccessible(preferences.app.accessMode, {
    ...options,
    routes: configuredRoutes,
    fetchMenuListAsync: async () => {
      ElMessage({
        duration: 1500,
        message: `${$t('common.loadingMenu')}...`,
      });
      return await getAllMenusApi();
    },
    // 可以指定没有权限跳转403页面
    forbiddenComponent,
    // 如果 route.meta.menuVisibleWithForbidden = true
    layoutMap,
    pageMap,
  });
}

export { generateAccess };
