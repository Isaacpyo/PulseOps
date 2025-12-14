import React from 'react';
import { ArrowRight } from 'lucide-react';

export default function Home() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-between p-24 bg-slate-50">
      <div className="z-10 w-full items-center justify-between font-mono text-sm lg:flex">
        <p className="fixed left-0 top-0 flex w-full justify-center border-b border-gray-300 bg-gradient-to-b from-zinc-200 pb-6 pt-8 backdrop-blur-2xl lg:static lg:w-auto lg:rounded-xl lg:border lg:bg-gray-200 lg:p-4">
          Status: <span className="font-bold text-green-600 ml-2">Live Demo</span>
        </p>
      </div>
      <div className="relative flex flex-col place-items-center text-center">
        <h1 className="text-6xl font-bold tracking-tight text-slate-900 mb-4">PulseOps</h1>
        <p className="text-xl text-slate-600 max-w-2xl">DevOps Monitoring & Incident Management System</p>
        <div className="mt-8 flex gap-4">
            <button className="px-6 py-3 bg-blue-600 text-white rounded-lg font-semibold hover:bg-blue-700 transition flex items-center gap-2">
                Launch App <ArrowRight size={18} />
            </button>
        </div>
      </div>
    </main>
  );
}
