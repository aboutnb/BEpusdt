<template>
  <a-layout-footer class="footer">
    <div class="footer_title">
      <span @click="onBEpusdt" class="primary-copyright">{{ currentYear }} © BEpusdt. All rights reserved.</span>
    </div>
  </a-layout-footer>
</template>

<script setup lang="ts">
const currentYear = ref(new Date().getFullYear());
const onBEpusdt = () => {
  window.open("https://github.com/v03413/BEpusdt", "_blank");
};
</script>

<style lang="scss" scoped>
.footer {
  box-sizing: border-box;
  display: flex;
  align-items: center;
  height: 30px;
  border-top: $border-1 solid $color-border-2;
  padding: 0 16px;
}

.footer_title {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;

  .primary-copyright {
    color: $color-text-2;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s ease;
    flex: 0 1 auto;
    text-align: center;

    &:hover {
      color: $color-primary;
      transform: translateY(-1px);
    }
  }
}

@media (max-width: 768px) {
  .footer {
    height: 32px;
    padding: 0 12px;
  }

  .footer_title {
    justify-content: center;

    .primary-copyright {
      flex: 0 1 auto;
      font-size: 11px;
      white-space: nowrap;
    }
  }
}
</style>
