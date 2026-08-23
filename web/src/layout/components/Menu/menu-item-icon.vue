<template>
  <component v-if="lineIcon" :is="lineIcon" class="menu-line-icon" :size="18" />
  <component v-else-if="props.icon" :is="props.icon" class="menu-line-icon" :size="18" />
</template>

<script setup lang="ts">
import type { Component } from "vue";
import { IconBook, IconDashboard, IconFile, IconList, IconSettings, IconStorage, IconSwap } from "@arco-design/web-vue/es/icon";

const props = defineProps({
  svgIcon: {
    type: String,
    default: ""
  },
  icon: {
    type: String,
    default: ""
  }
});

const lineIcons: Record<string, Component> = {
  about: IconBook,
  classify: IconStorage,
  directives: IconSwap,
  form: IconFile,
  home: IconDashboard,
  set: IconSettings,
  table: IconList
};

const lineIcon = computed(() => lineIcons[props.svgIcon]);
</script>

<style lang="scss" scoped>
.menu-line-icon {
  flex: 0 0 auto;
  color: currentcolor;
  stroke-width: 3;
}
</style>
