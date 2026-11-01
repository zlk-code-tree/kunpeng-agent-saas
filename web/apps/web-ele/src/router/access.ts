import type { RouteRecordRaw } from 'vue-router';

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

/**
 * 将数据库菜单配置递归应用到已注册路由。
 * 系统管理改为父子结构后，不能只处理顶层路由，否则子菜单的标题、排序和显隐不会生效。
 */
function applyMenuSettings(
  routes: RouteRecordRaw[],
  byName: Map<string, Awaited<ReturnType<typeof listMenuSettingsApi>>[number]>,
): RouteRecordRaw[] {
  return routes
    .filter((route) => {
      const setting = byName.get(String(route.name));
      return !setting || setting.enabled || setting.protected;
    })
    .map((route) => {
      const setting = byName.get(String(route.name));
      return {
        ...route,
        ...(route.children
          ? { children: applyMenuSettings(route.children, byName) }
          : {}),
        ...(setting
          ? {
              meta: {
                ...route.meta,
                order: setting.sortOrder,
                title: setting.title,
              },
            }
          : {}),
      } as RouteRecordRaw;
    });
}

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
    configuredRoutes = applyMenuSettings(options.routes, byName);
    // 自定义菜单（menu_type='link'）注入为真实路由：
    // window 模式走 meta.link 新窗口打开，iframe 模式用 IFrameView 内嵌展示。
    const customRoutes: RouteRecordRaw[] = settings
      .filter((item) => item.menuType === 'link' && item.enabled)
      .map((item) => ({
        component: IFrameView,
        meta: {
          icon: item.icon || undefined,
          order: item.sortOrder,
          title: item.title,
          ...(item.openMode === 'iframe'
            ? { iframeSrc: item.linkUrl }
            : { link: item.linkUrl }),
        },
        name: item.routeName,
        path: `/custom/${item.routeName}`,
      }));
    configuredRoutes = [...configuredRoutes, ...customRoutes];
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
