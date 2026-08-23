<template>
  <div class="banner_box">
    <div class="banner_title">
      <s-svg-icon name="bepusdt" size="32" color="#64d8cf" />
      <span>{{ title }}</span>
    </div>
    <div class="banner-copy">
      <span class="banner-kicker">PAYMENT OPERATIONS</span>
      <h1>每一笔链上收款<br />都保持清晰可见</h1>
      <div class="banner-rule"><span></span></div>
    </div>
    <div class="banner_img">
      <s-svg-icon name="数据时代" size="100%" />
    </div>
  </div>
</template>
<script setup lang="ts">
// 全局title
const title = import.meta.env.VITE_GLOB_APP_TITLE;
</script>

<style lang="scss" scoped>
.banner_box {
  position: relative;
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  width: 650px;
  height: 100%;
  flex: 1;
  padding: 34px 42px;
  overflow: hidden;
  color: #ffffff;
  background-color: #102a43;
  background-image:
    linear-gradient(rgb(255 255 255 / 5%) 1px, transparent 1px),
    linear-gradient(90deg, rgb(255 255 255 / 5%) 1px, transparent 1px), linear-gradient(135deg, #102a43, #163b5c 68%, #145c5e);
  background-size:
    32px 32px,
    32px 32px,
    100% 100%;

  .banner_title {
    display: flex;
    gap: 10px;
    align-items: center;
    color: #ffffff;
    font-size: 19px;
    font-weight: 700;
    letter-spacing: 0.02em;
  }

  .banner-copy {
    position: relative;
    z-index: 1;
    margin-top: 58px;

    h1 {
      margin: 10px 0 18px;
      color: #f5fbff;
      font-size: 31px;
      font-weight: 650;
      line-height: 1.35;
      letter-spacing: 0.02em;
    }
  }

  .banner-kicker {
    color: #64d8cf;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.2em;
  }

  .banner-rule {
    width: 120px;
    height: 3px;
    background: rgb(255 255 255 / 12%);

    span {
      display: block;
      width: 48px;
      height: 100%;
      background: #d9822b;
    }
  }

  .banner_img {
    position: absolute;
    right: 34px;
    bottom: 40px;
    width: 330px;
    height: 270px;
    padding: 16px;
    border: 1px solid rgb(255 255 255 / 10%);
    border-radius: 12px;
    background: rgb(255 255 255 / 92%);
    box-shadow: 0 24px 60px rgb(0 0 0 / 22%);
    transform: rotate(-2deg);
  }
}
</style>
