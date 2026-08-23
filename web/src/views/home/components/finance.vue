<template>
  <div class="shortcut-box">
    <div class="box-title">
      <div>数据汇总</div>
    </div>
    <a-divider :margin="16" />
    <a-grid class="finance-card" :cols="{ xs: 2, sm: 2, lg: 3, xl: 5 }" :col-gap="12" :row-gap="12">
      <a-grid-item v-for="item in financeData" :key="item.id">
        <a-card class="finance-a-card" :class="{ 'is-period': item.id === 5 }">
          <div class="finance-nav">
            <span class="finance-nav-title">{{ item.title }}</span>
          </div>
          <div class="finance-value">{{ item.value }}</div>
          <div class="finance-sub" :class="{ single: !item.subLabel }">
            <span v-if="item.subLabel">{{ item.subLabel }}</span>
            <strong>{{ item.subValue }}</strong>
          </div>
        </a-card>
      </a-grid-item>
    </a-grid>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  homeData: any;
}>();

const formatAmount = (value: any) => Number(value || 0).toFixed(2);

const formatPeriod = (from?: string, to?: string) => {
  if (!from || !to) return "--";
  return `${from.slice(5, 10).replace("-", "/")} - ${to.slice(5, 10).replace("-", "/")}`;
};

const buildFinanceData = (data: any) => {
  const kpi = data?.kpi || {};
  return [
    {
      id: 1,
      title: "订单总数",
      value: kpi.orders_total || 0,
      subLabel: "已支付订单",
      subValue: kpi.orders_success || 0
    },
    {
      id: 2,
      title: "收款金额",
      value: formatAmount(kpi.gmv_paid),
      subLabel: "支付成功率",
      subValue: `${formatAmount(kpi.order_success_rate)}%`
    },
    {
      id: 3,
      title: "待付订单",
      value: kpi.orders_pending || 0,
      subLabel: "确认中订单",
      subValue: kpi.orders_confirming || 0
    },
    {
      id: 4,
      title: "失败订单",
      value: kpi.orders_failed || 0,
      subLabel: "通知失败",
      subValue: kpi.notify_failed || 0
    },
    {
      id: 5,
      title: "统计周期",
      value: formatPeriod(data?.from, data?.to),
      subLabel: "",
      subValue: data?.timezone || "--"
    }
  ];
};

const financeData = ref(buildFinanceData(props.homeData));

watch(
  () => props.homeData,
  newData => {
    financeData.value = buildFinanceData(newData);
  },
  { immediate: true }
);
</script>

<style lang="scss" scoped>
.shortcut-box {
  .card-box {
    margin-bottom: $padding;
    .shortcut-card-label {
      width: 100px;
      margin-left: 20px;
      font-size: $font-size-body-3;
      color: $color-text-2;
    }
  }
  .card-middling {
    width: 200px;
  }
  .row-center {
    display: flex;
    align-items: center;
    justify-content: center;
  }
}
.box-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: $font-size-body-3;
  color: $color-text-1;
}
.finance-a-card {
  box-sizing: border-box;
  height: 126px;
  border-color: var(--console-line, #e5e6eb);
  background: #ffffff;

  :deep(.arco-card-body) {
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    height: 100%;
    padding: 16px 18px 14px;
  }
}
.finance-nav-title {
  color: #6b7785;
  font-size: 13px;
  font-weight: 500;
}
.finance-value {
  margin: 7px 0 0;
  color: $color-text-1;
  font-size: 26px;
  line-height: 34px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  word-break: break-word;
}
.finance-sub {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: auto;
  padding-top: 8px;
  border-top: 1px solid #f2f3f5;
  color: #86909c;
  font-size: 12px;
  line-height: 17px;
  white-space: nowrap;

  strong {
    color: #4e5969;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
  }

  &.single {
    justify-content: flex-start;
  }
}
.margin-left-text {
  margin-left: $margin-text;
}

@media (max-width: 768px) {
  .finance-card :deep(.arco-grid-item:last-child) {
    grid-column: span 2 !important;
  }

  .finance-a-card {
    height: 118px;

    :deep(.arco-card-body) {
      padding: 14px;
    }

    &.is-period .finance-value {
      font-size: 21px;
    }
  }

  .finance-value {
    font-size: 24px;
  }
}
</style>
