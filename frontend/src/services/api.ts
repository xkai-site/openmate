import axios, { AxiosError, type InternalAxiosRequestConfig } from 'axios';
import { message } from 'antd';

interface BusinessPayload<T = unknown> {
  code?: number;
  message?: string;
  data?: T;
}

const statusMessageMap: Record<number, string> = {
  400: '请求参数错误',
  404: '资源不存在',
  409: '资源冲突',
  500: '服务器内部错误',
};

// 为请求配置扩展时间戳字段（用于计算耗时）
interface TimedRequestConfig extends InternalAxiosRequestConfig {
  _startTime?: number;
}

const isDev = import.meta.env.DEV;

const logger = {
  request: (config: TimedRequestConfig) => {
    if (!isDev) return;
    const { method, url, params, data } = config;
    console.groupCollapsed(
      `%c[API] %c${method?.toUpperCase()} %c${url}`,
      'color:#888',
      'color:#4CAF50;font-weight:bold',
      'color:#2196F3',
    );
    if (params) console.log('Params:', params);
    if (data) console.log('Body:', typeof data === 'string' ? JSON.parse(data) : data);
    console.groupEnd();
  },
  response: (url: string | undefined, status: number, duration: number, data: unknown) => {
    if (!isDev) return;
    console.groupCollapsed(
      `%c[API] %c${status} %c${url} %c(${duration}ms)`,
      'color:#888',
      'color:#4CAF50;font-weight:bold',
      'color:#2196F3',
      'color:#888',
    );
    console.log('Response:', data);
    console.groupEnd();
  },
  error: (url: string | undefined, status: number | string, duration: number, error: unknown) => {
    if (!isDev) return;
    console.groupCollapsed(
      `%c[API] %c${status} %c${url} %c(${duration}ms)`,
      'color:#888',
      'color:#F44336;font-weight:bold',
      'color:#2196F3',
      'color:#888',
    );
    console.error('Error:', error);
    console.groupEnd();
  },
};

export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || '/api/v1';

export const api = axios.create({
  baseURL: API_BASE_URL,
  timeout: 60000,
});


// 请求拦截器：记录请求开始时间
api.interceptors.request.use((config: TimedRequestConfig) => {
  config._startTime = Date.now();
  logger.request(config);
  return config;
});

api.interceptors.response.use(
  (resp) => {
    const config = resp.config as TimedRequestConfig;
    const duration = config._startTime ? Date.now() - config._startTime : 0;
    const payload = resp.data as BusinessPayload;

    if (typeof payload?.code === 'number' && payload.code !== 200) {
      const errText = payload.message || '业务请求失败';
      logger.error(config.url, payload.code, duration, errText);
      message.error(errText);
      return Promise.reject(new Error(errText));
    }

    logger.response(config.url, resp.status, duration, resp.data);
    // 返回 resp.data 而不是 resp，让业务代码直接使用 response.data
    return resp.data;
  },
  (error: AxiosError<BusinessPayload>) => {
    const config = error.config as TimedRequestConfig | undefined;
    const duration = config?._startTime ? Date.now() - config._startTime : 0;
    const status = error.response?.status;
    const serverMsg = error.response?.data?.message;
    const msg = status
      ? `${statusMessageMap[status] || '请求失败'}: ${serverMsg || '未知错误'}`
      : '网络异常，请检查服务是否可用';

    logger.error(config?.url, status ?? 'ERR', duration, error);
    message.error(msg);
    return Promise.reject(new Error(msg));
  },
);