// Seeded demo accounts (docs/13 §6).
export const ADMIN = {
  email: "admin@athenaeum.dev",
  password: "athenaeum-demo",
  name: "Library Admin",
};
export const DEMO = {
  email: "demo@athenaeum.dev",
  password: "athenaeum-demo",
  name: "Demo Patron",
};

export const AUTH_DIR = "e2e/.auth";
export const ADMIN_STATE = `${AUTH_DIR}/admin.json`;
export const DEMO_STATE = `${AUTH_DIR}/demo.json`;
