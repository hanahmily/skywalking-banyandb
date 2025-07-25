<!--
  ~ Licensed to Apache Software Foundation (ASF) under one or more contributor
  ~ license agreements. See the NOTICE file distributed with
  ~ this work for additional information regarding copyright
  ~ ownership. Apache Software Foundation (ASF) licenses this file to you under
  ~ the Apache License, Version 2.0 (the "License"); you may
  ~ not use this file except in compliance with the License.
  ~ You may obtain a copy of the License at
  ~
  ~     http://www.apache.org/licenses/LICENSE-2.0
  ~
  ~ Unless required by applicable law or agreed to in writing,
  ~ software distributed under the License is distributed on an
  ~ "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
  ~ KIND, either express or implied.  See the License for the
  ~ specific language governing permissions and limitations
  ~ under the License.
-->

<script lang="ts" setup>
  import { reactive, ref } from 'vue';
  import { watch, getCurrentInstance } from '@vue/runtime-core';
  import { useRoute, useRouter } from 'vue-router';
  import type { FormInstance } from 'element-plus';
  import { createSecondaryDataModel, getSecondaryDataModel, updateSecondaryDataModel } from '@/api/index';
  import { ElMessage } from 'element-plus';
  import FormHeader from '../common/FormHeader.vue';

  const $loadingCreate = getCurrentInstance().appContext.config.globalProperties.$loadingCreate;
  const $loadingClose = getCurrentInstance().appContext.config.globalProperties.$loadingClose;
  const $bus = getCurrentInstance().appContext.config.globalProperties.mittBus;
  const ruleFormRef = ref<FormInstance>();

  const route = useRoute();
  const router = useRouter();
  const rules = {
    group: [
      {
        required: true,
        message: 'Please enter the group',
        trigger: 'blur',
      },
    ],
    name: [
      {
        required: true,
        message: 'Please enter the name',
        trigger: 'blur',
      },
    ],
    analyzer: [
      {
        required: true,
        message: 'Please select the analyzer',
        trigger: 'blur',
      },
    ],
    tags: [
      {
        required: true,
        message: 'Please select the tags',
        trigger: 'blur',
      },
    ],
    type: [
      {
        required: true,
        message: 'Please select the type',
        trigger: 'blur',
      },
    ],
  };
  const typeList = [
    {
      label: 'TYPE_INVERTED',
      value: 'TYPE_INVERTED',
    },
    {
      label: 'TYPE_SKIPPING',
      value: 'TYPE_SKIPPING',
    },
    {
      label: 'TYPE_TREE',
      value: 'TYPE_TREE',
    },
  ];
  const analyzerList = [
    {
      label: 'unspecified',
      value: '',
    },
    {
      label: 'keyword',
      value: 'keyword',
    },
    {
      label: 'standard',
      value: 'standard',
    },
    {
      label: 'simple',
      value: 'simple',
    },
  ];
  const data = reactive({
    group: route.params.group,
    name: route.params.name,
    type: route.params.type,
    operator: route.params.operator,
    schema: route.params.schema,
    form: {
      group: route.params.group,
      name: route.params.name || '',
      analyzer: '',
      tags: [],
      type: '',
    },
  });

  watch(
    () => route,
    () => {
      data.form = {
        group: route.params.group,
        name: route.params.name || '',
        analyzer: '',
        tags: [],
        type: '',
      };
      data.group = route.params.group;
      data.name = route.params.name;
      data.type = route.params.type;
      data.operator = route.params.operator;
      data.schema = route.params.schema;
      initData();
    },
    {
      immediate: true,
      deep: true,
    },
  );
  const submit = async (formEl: FormInstance | undefined) => {
    if (!formEl) return;
    await formEl.validate((valid) => {
      if (valid) {
        const param = {
          indexRule: {
            metadata: {
              group: data.form.group,
              name: data.form.name,
            },
            tags: data.form.tags,
            type: data.form.type,
            analyzer: data.form.analyzer,
          },
        };
        $loadingCreate();
        if (data.operator === 'create') {
          return createSecondaryDataModel('index-rule', param)
            .then((res) => {
              if (res.status === 200) {
                ElMessage({
                  message: 'Create successed',
                  type: 'success',
                  duration: 5000,
                });
                $bus.emit('refreshAside');
                $bus.emit('deleteGroup', data.form.group);
                openIndexRule();
              }
            })
            .catch((err) => {
              ElMessage({
                message: 'Please refresh and try again. Error: ' + err,
                type: 'error',
                duration: 3000,
              });
            })
            .finally(() => {
              $loadingClose();
            });
        } else {
          return updateSecondaryDataModel('index-rule', data.form.group, data.form.name, param)
            .then((res) => {
              if (res.status === 200) {
                ElMessage({
                  message: 'Edit successed',
                  type: 'success',
                  duration: 5000,
                });
                $bus.emit('refreshAside');
                $bus.emit('deleteResource', data.form.group);
                openIndexRule();
              }
            })
            .catch((err) => {
              ElMessage({
                message: 'Please refresh and try again. Error: ' + err,
                type: 'error',
                duration: 3000,
              });
            })
            .finally(() => {
              $loadingClose();
            });
        }
      }
    });
  };

  function openIndexRule() {
    const route = {
      name: data.schema + '-' + data.type,
      params: {
        group: data.form.group,
        name: data.form.name,
        operator: 'read',
        type: data.type + '',
      },
    };
    router.push(route);
    const add = {
      label: data.form.name,
      type: `Read-${data.type}`,
      route,
    };
    $bus.emit('AddTabs', add);
  }

  function initData() {
    if (data.operator === 'edit' && data.form.group && data.form.name) {
      $loadingCreate();
      getSecondaryDataModel('index-rule', data.form.group, data.form.name)
        .then((res) => {
          if (res.status === 200) {
            const indexRule = res.data.indexRule;
            data.form = {
              group: indexRule.metadata.group,
              name: indexRule.metadata.name,
              analyzer: indexRule.analyzer,
              tags: indexRule.tags,
              type: indexRule.type,
            };
          }
        })
        .catch((err) => {
          ElMessage({
            message: 'Please refresh and try again. Error: ' + err,
            type: 'error',
            duration: 3000,
          });
        })
        .finally(() => {
          $loadingClose();
        });
    }
  }
</script>

<template>
  <div>
    <el-card>
      <template #header>
        <el-row>
          <el-col :span="20">
            <FormHeader :fields="data" />
          </el-col>
          <el-col :span="4">
            <div class="flex align-item-center justify-end" style="height: 30px">
              <el-button size="small" type="primary" @click="submit(ruleFormRef)" color="#6E38F7">Submit</el-button>
            </div>
          </el-col>
        </el-row>
      </template>
      <el-form
        ref="ruleFormRef"
        :rules="rules"
        label-position="left"
        label-width="100px"
        :model="data.form"
        style="width: 90%"
      >
        <el-form-item label="Group" prop="group">
          <el-input v-model="data.form.group" clearable :disabled="true"></el-input>
        </el-form-item>
        <el-form-item label="Name" prop="name">
          <el-input
            v-model="data.form.name"
            :disabled="data.operator === 'edit'"
            clearable
            placeholder="Input Name"
          ></el-input>
        </el-form-item>
        <el-form-item label="Analyzer" prop="analyzer">
          <el-select v-model="data.form.analyzer" placeholder="Choose Analyzer" style="width: 100%" clearable>
            <el-option v-for="item in analyzerList" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="Tags" prop="tags">
          <el-select
            class="tags-and-rules"
            v-model="data.form.tags"
            allow-create
            filterable
            default-first-option
            placeholder="Input Tags"
            style="width: 100%"
            clearable
            multiple
          ></el-select>
        </el-form-item>
        <el-form-item label="Type" prop="type">
          <el-select v-model="data.form.type" placeholder="Choose Type" style="width: 100%" clearable>
            <el-option v-for="item in typeList" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<style lang="scss" scoped>
  :deep(.el-card) {
    margin: 15px;
  }
</style>
