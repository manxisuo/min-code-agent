<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import { apiSkill, apiSkills } from "../skillApi";
import type { SkillDetail, SkillListItem } from "../types";
import MdText from "./MdText.vue";

const skills = ref<SkillListItem[]>([]);
const skillsDir = ref("skills/");
const skillsDirAbs = ref("");
const activeCount = ref(0);
const contextChars = ref(0);
const error = ref("");
const selected = ref<SkillDetail | null>(null);
const loading = ref(false);

async function refresh() {
  error.value = "";
  try {
    const data = await apiSkills();
    skills.value = data.skills || [];
    skillsDir.value = data.dir || "skills/";
    skillsDirAbs.value = data.skills_dir || "";
    activeCount.value = data.active_count || 0;
    contextChars.value = data.context_chars || 0;
  } catch (e) {
    error.value = String((e as Error).message || e);
    skills.value = [];
  }
}

async function open(name: string) {
  loading.value = true;
  error.value = "";
  try {
    selected.value = await apiSkill(name);
  } catch (e) {
    error.value = String((e as Error).message || e);
    selected.value = null;
  } finally {
    loading.value = false;
  }
}

function closeDetail() {
  selected.value = null;
}

let poll: number | null = null;
onMounted(() => {
  void refresh();
  poll = window.setInterval(() => void refresh(), 4000);
});
onBeforeUnmount(() => {
  if (poll != null) window.clearInterval(poll);
});
</script>

<template>
  <section class="panel skills-panel">
    <div class="panel-head">
      <h2>Skills</h2>
      <div class="head-actions">
        <span class="hint">{{ skillsDir }} · {{ skills.length }} available</span>
        <button type="button" class="linkish" @click="refresh()">Refresh</button>
      </div>
    </div>

    <div v-if="error" class="exp-error">{{ error }}</div>
    <div v-if="skillsDirAbs" class="skills-meta">
      dir: {{ skillsDirAbs }} · in context: {{ activeCount }} skill(s), {{ contextChars }} chars
    </div>

    <div class="skills-body">
      <div class="skills-list">
        <div v-if="!skills.length && !error" class="empty">
          no skills found — put them in
          <code>skills/&lt;name&gt;/SKILL.md</code>
        </div>
        <button
          v-for="sk in skills"
          :key="sk.name"
          type="button"
          class="skill-card"
          :class="{ active: sk.active, selected: selected?.name === sk.name }"
          @click="open(sk.name)"
        >
          <div class="skill-name">
            <span class="mark">{{ sk.active ? "●" : "○" }}</span>
            {{ sk.name }}
            <span v-if="sk.active" class="tag-on">active</span>
          </div>
          <div v-if="sk.summary" class="skill-sum">{{ sk.summary }}</div>
          <div class="skill-path">{{ sk.rel_path }} · {{ sk.bytes }}B</div>
        </button>
      </div>

      <div class="skills-detail">
        <div v-if="!selected" class="empty">
          选择左侧 skill 查看 SKILL.md 全文（与 CLI /skills 列表对应）
        </div>
        <template v-else>
          <div class="skills-detail-head">
            <div>
              <b>{{ selected.name }}</b>
              <span v-if="selected.active" class="tag-on">active</span>
              <div class="hint">{{ selected.rel_path }}</div>
            </div>
            <button type="button" class="linkish" @click="closeDetail()">关闭</button>
          </div>
          <div class="skills-md">
            <MdText :content="selected.content" />
          </div>
        </template>
      </div>
    </div>
  </section>
</template>
