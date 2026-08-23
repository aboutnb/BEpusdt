<template>
  <div class="snow-page">
    <div class="home-page">
      <div class="overview-intro">
        <div class="overview-copy">
          <h1>运营总览</h1>
          <p>收款、订单与资产概览</p>
        </div>
        <div class="overview-sync">
          <span class="sync-dot" :class="{ syncing: loading }"></span>
          <div>
            <strong>{{ loading ? "正在同步" : "数据已同步" }}</strong>
            <span>{{ timezone }} · {{ activeRangeLabel }}</span>
          </div>
        </div>
      </div>
      <div class="dashboard-toolbar">
        <!-- 法币选择器 -->
        <div class="fiat-selector">
          <span class="label">交易法币：</span>
          <div class="fiat-options">
            <div
              v-for="item in fiatOptions"
              :key="item.value"
              class="fiat-option"
              :class="{ active: fiat === item.value }"
              @click="handleFiatChange(item.value)"
            >
              <span class="currency-symbol">{{ item.symbol }}</span>
              <span class="currency-name">{{ item.label }}</span>
            </div>
          </div>
        </div>

        <div class="range-actions">
          <a-range-picker
            v-if="range === 'custom'"
            v-model="customDates"
            format="YYYY-MM-DD"
            style="width: 240px"
            @change="handleCustomDateChange"
          />
          <a-select v-model="range" style="width: 132px" @change="handleRangeChange">
            <a-option v-for="item in rangeOptions" :key="item.value" :value="item.value">
              {{ item.label }}
            </a-option>
          </a-select>
          <a-button type="primary" :loading="loading" @click="forceRefresh">
            <template #icon><icon-refresh /></template>
            强制刷新
          </a-button>
        </div>
      </div>

      <!-- 财务指标 -->
      <Finance :home-data="home" />
      <!-- 数据图 -->
      <DataBox :home-data="home" />
    </div>
  </div>
</template>

<script setup lang="ts">
import Finance from "@/views/home/components/finance.vue";
import DataBox from "@/views/home/components/data-box.vue";
import { getDashboardHomeAPI } from "@/api/modules/home/index";

const fiat = ref("CNY");
const range = ref("7d");
const customDates = ref<any[]>([]);
const home = ref<any>(null);
const loading = ref(false);
const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || "Asia/Shanghai";
let dashboardRetryTimer: ReturnType<typeof setTimeout> | null = null;

const fiatOptions = ref([
  { value: "CNY", label: "人民币", symbol: "¥" },
  { value: "USD", label: "美元", symbol: "$" },
  { value: "EUR", label: "欧元", symbol: "€" },
  { value: "GBP", label: "英镑", symbol: "£" },
  { value: "JPY", label: "日元", symbol: "¥" }
]);

const rangeOptions = [
  { value: "today", label: "今天" },
  { value: "7d", label: "最近 7 天" },
  { value: "30d", label: "最近 30 天" },
  { value: "custom", label: "自定义" }
];

const activeRangeLabel = computed(() => rangeOptions.find(item => item.value === range.value)?.label || "自定义周期");

const clearDashboardRetry = () => {
  if (dashboardRetryTimer) {
    clearTimeout(dashboardRetryTimer);
    dashboardRetryTimer = null;
  }
};

const getDashboardHome = async (force = false, retryCount = 0) => {
  if (range.value === "custom" && (!Array.isArray(customDates.value) || customDates.value.length !== 2)) return;

  if (retryCount === 0) {
    clearDashboardRetry();
  }

  loading.value = true;
  try {
    const params: any = {
      range: range.value,
      tz: timezone,
      fiat: fiat.value,
      force
    };
    if (range.value === "custom") {
      params.from = customDates.value[0];
      params.to = customDates.value[1];
    }

    const data = await getDashboardHomeAPI(params);
    if (!data?.data) {
      throw new Error("仪表盘数据为空");
    }
    home.value = data.data;
  } catch (error) {
    if (retryCount < 3) {
      dashboardRetryTimer = setTimeout(
        () => {
          getDashboardHome(force, retryCount + 1);
        },
        (retryCount + 1) * 1000
      );
      return;
    }
    console.error("获取首页统计失败:", error);
  } finally {
    loading.value = false;
  }
};

// 处理法币切换
const handleFiatChange = (value: string) => {
  fiat.value = value;
  getDashboardHome();
};

const handleRangeChange = () => {
  if (range.value !== "custom") {
    getDashboardHome();
  }
};

const handleCustomDateChange = () => {
  getDashboardHome();
};

const forceRefresh = () => {
  getDashboardHome(true);
};

onMounted(() => {
  getDashboardHome();
});

onUnmounted(() => {
  clearDashboardRetry();
});
</script>

<style lang="scss" scoped>
.home-page {
  padding: 0;
  background: transparent;
}

.overview-intro {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
  padding: 2px 0 14px;
  border-bottom: 1px solid var(--console-line, #e5e6eb);

  h1 {
    margin: 0;
    color: var(--console-ink, #1d2129);
    font-size: 22px;
    line-height: 1.3;
    letter-spacing: 0;
  }

  p {
    margin: 0;
    color: #86909c;
    font-size: 13px;
  }
}

.overview-copy {
  display: flex;
  align-items: baseline;
  gap: 12px;
}

.overview-sync {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  padding: 0;
  border: 0;
  background: transparent;

  strong,
  span {
    display: inline;
  }

  strong {
    color: #86909c;
    font-size: 12px;
    font-weight: 400;

    &::after {
      content: " · ";
    }
  }

  span:not(.sync-dot) {
    margin: 0;
    color: #86909c;
    font-size: 12px;
  }
}

.sync-dot {
  width: 7px;
  height: 7px;
  flex: 0 0 7px;
  border-radius: 50%;
  background: #00b42a;
  box-shadow: none;

  &.syncing {
    background: #ff7d00;
    box-shadow: none;
    animation: none;
  }
}

.dashboard-toolbar {
  margin-bottom: 16px;
  padding-bottom: 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  border-bottom: 1px solid var(--console-line, #e5e6eb);
}

.fiat-selector {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0;
  background: transparent;
  border: 0;
  border-radius: 0;
  box-shadow: none;

  .label {
    font-size: 13px;
    font-weight: 500;
    color: $color-text-2;
    white-space: nowrap;
  }

  .fiat-options {
    display: flex;
    gap: 6px;
  }

  .fiat-option {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 6px 12px;
    border-radius: 4px;
    cursor: pointer;
    transition: all 0.2s ease;
    border: 1px solid $color-border-2;
    background: $color-fill-1;
    min-width: 60px;
    justify-content: center;

    &:hover {
      border-color: $color-primary;
      background: rgb(var(--primary-1));
    }

    &.active {
      background: $color-primary;
      border-color: $color-primary;
      color: #fff;
      box-shadow: none;

      .currency-symbol,
      .currency-name {
        color: #fff;
      }
    }

    .currency-symbol {
      font-size: 14px;
      font-weight: bold;
      color: $color-primary;
    }

    .currency-name {
      font-size: 11px;
      color: $color-text-2;
      font-weight: 500;
    }
  }
}

.range-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  margin-left: auto;
}

// 响应式设计
@media (max-width: 768px) {
  .overview-intro {
    align-items: flex-start;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 14px;

    h1 {
      font-size: 20px;
    }
  }

  .overview-copy {
    align-items: flex-start;
    flex-direction: column;
    gap: 4px;
  }

  .overview-sync {
    box-sizing: border-box;
    width: auto;
  }

  .dashboard-toolbar {
    align-items: stretch;
  }

  .fiat-selector {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
    padding: 10px 12px;

    .fiat-options {
      width: 100%;
      justify-content: space-between;
      flex-wrap: wrap;
    }

    .fiat-option {
      flex: 1;
      min-width: auto;
      padding: 6px 4px;
      margin-bottom: 4px;
      white-space: nowrap;

      .currency-name {
        white-space: nowrap;
      }
    }
  }

  .range-actions {
    width: 100%;
    flex-wrap: wrap;
    justify-content: flex-start;
    margin-left: 0;
  }
}
</style>
