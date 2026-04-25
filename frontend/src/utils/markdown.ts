export function closeOpenCodeFences(content: string): string {
  const fences = content.match(/^```/gm);
  if (fences && fences.length % 2 !== 0) {
    // 流式阶段代码块尚未闭合时，临时补齐闭合 fence，避免渲染异常
    return `${content}\n\`\`\``;
  }
  return content;
}

