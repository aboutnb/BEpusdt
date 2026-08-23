<template>
  <div class="container">
    <div class="login">
      <LoginBanner v-if="isPc" />
      <div class="login_box">
        <div class="secure-entry"><span></span> 安全入口已验证</div>
        <div class="login_title">管理员登录</div>
        <div class="login_title_desc">进入 BEpusdt 收款控制台</div>
        <LoginForm />
        <div class="login-footnote">BEpusdt · Payment Gateway</div>
      </div>
    </div>
  </div>
</template>
<script setup lang="ts">
import LoginBanner from "@/views/login/components/login-banner.vue";
import LoginForm from "@/views/login/components/login-form.vue";
import { useDevicesSize } from "@/hooks/useDevicesSize";
const { isPc } = useDevicesSize();
</script>

<style lang="scss" scoped>
.container {
  position: relative;
  display: grid;
  place-items: center;
  height: 100vh;
  overflow: hidden;
  background-color: #eef3f6;
  background-image:
    linear-gradient(rgb(16 42 67 / 4%) 1px, transparent 1px), linear-gradient(90deg, rgb(16 42 67 / 4%) 1px, transparent 1px);
  background-size: 32px 32px;

  &::before {
    position: absolute;
    inset: 0;
    content: "";
    background: linear-gradient(120deg, rgb(255 255 255 / 78%), transparent 50%, rgb(26 166 166 / 5%));
    pointer-events: none;
  }

  .login {
    position: relative;
    display: flex;
    align-items: center;
    width: min(1040px, calc(100vw - 48px));
    height: min(580px, calc(100vh - 72px));
    min-height: 520px;
    overflow: hidden;
    border: 1px solid rgb(16 42 67 / 10%);
    border-radius: 18px;
    background: #ffffff;
    box-shadow: 0 28px 70px rgb(16 42 67 / 16%);

    .login_box {
      position: relative;
      box-sizing: border-box;
      width: 390px;
      height: 100%;
      flex: 0 0 390px;
      padding: 86px 42px 38px;

      .login_title {
        margin: 18px 0 7px;
        color: #102a43;
        font-size: 28px;
        font-weight: 700;
        line-height: 1.2;
        letter-spacing: 0.01em;
      }

      .login_title_desc {
        color: #627d98;
        font-size: 13px;
      }

      .secure-entry {
        display: inline-flex;
        align-items: center;
        gap: 7px;
        color: #187467;
        font-size: 12px;
        font-weight: 600;

        span {
          width: 7px;
          height: 7px;
          border-radius: 50%;
          background: #20a779;
          box-shadow: 0 0 0 4px rgb(32 167 121 / 12%);
        }
      }

      .login-footnote {
        position: absolute;
        bottom: 30px;
        color: #9fb3c8;
        font-size: 11px;
        letter-spacing: 0.08em;
      }
    }
  }
}

@media (max-width: 1024px) {
  .container .login {
    width: min(440px, calc(100vw - 28px));
    height: min(590px, calc(100vh - 32px));
    min-height: 520px;

    .login_box {
      width: 100%;
      flex-basis: 100%;
      padding: 72px 32px 34px;
    }
  }
}

@media (max-width: 480px) {
  .container .login {
    border-radius: 12px;

    .login_box {
      padding: 58px 24px 28px;

      .login_title {
        font-size: 25px;
      }
    }
  }
}
</style>
