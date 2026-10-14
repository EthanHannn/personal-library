import { createRouter, createWebHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: () => import('./views/HomeView.vue') },
    { path: '/library', component: () => import('./views/CatalogView.vue') },
    { path: '/category/:tag', component: () => import('./views/CatalogView.vue'), props: route => ({ kind: 'category', name: route.params.tag }) },
    { path: '/author/:name', component: () => import('./views/CatalogView.vue'), props: route => ({ kind: 'author', name: route.params.name }) },
    { path: '/bookshelf', component: () => import('./views/BookshelfView.vue') },
    { path: '/profile', component: () => import('./views/ProfileView.vue') },
    { path: '/manage', component: () => import('./views/ManageView.vue') },
    { path: '/book/:id(\\d+)', component: () => import('./views/BookView.vue') },
    { path: '/read/:id(\\d+)', component: () => import('./views/ReadView.vue') },
    { path: '/:pathMatch(.*)*', component: () => import('./views/NotFoundView.vue') },
  ],
  scrollBehavior: (_to, _from, saved) => saved ?? { top: 0 },
})
