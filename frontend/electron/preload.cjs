const { contextBridge, ipcRenderer } = require("electron");

const CHANNELS = {
  permission: {
    getWorkspace: "openmate:permission:get-workspace",
    selectWorkspace: "openmate:permission:select-workspace",
  },
  file: {
    read: "openmate:file:read",
    list: "openmate:file:list",
    write: "openmate:file:write",
    edit: "openmate:file:edit",
    patch: "openmate:file:patch",
    glob: "openmate:file:glob",
    grep: "openmate:file:grep",
  },
};

const bridge = {
  permission: {
    getWorkspace: () => ipcRenderer.invoke(CHANNELS.permission.getWorkspace),
    selectWorkspace: () => ipcRenderer.invoke(CHANNELS.permission.selectWorkspace),
  },
  file: {
    read: (payload) => ipcRenderer.invoke(CHANNELS.file.read, payload),
    list: (payload) => ipcRenderer.invoke(CHANNELS.file.list, payload),
    write: (payload) => ipcRenderer.invoke(CHANNELS.file.write, payload),
    edit: (payload) => ipcRenderer.invoke(CHANNELS.file.edit, payload),
    patch: (payload) => ipcRenderer.invoke(CHANNELS.file.patch, payload),
    glob: (payload) => ipcRenderer.invoke(CHANNELS.file.glob, payload),
    grep: (payload) => ipcRenderer.invoke(CHANNELS.file.grep, payload),
  },
};

contextBridge.exposeInMainWorld("openmate", bridge);
