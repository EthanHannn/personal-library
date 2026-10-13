import { createRouter, createWebHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/library' },
    { path: '/library', component: () => import('./views/CatalogView.vue') },
    { path: '/book/:id(\\d+)', component: () => import('./views/BookView.vue') },
    { path: '/read/:id(\\d+)', component: () => import('./views/ReadView.vue') },
    { path: '/:pathMatch(.*)*', component: () => import('./views/NotFoundView.vue') },
  ],
  scrollBehavior: (_to, _from, saved) => saved ?? { top: 0 },
})
